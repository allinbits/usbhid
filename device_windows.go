// Copyright 2022-2024 Rafael G. Martins. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package usbhid

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type overlapped struct {
	n       uint32
	ev      uintptr
	timeout time.Duration // no limit if zero
	ovl     struct {
		internal     uintptr
		internalHigh uintptr
		offset       uint32
		offsetHigh   uint32
		hEvent       uintptr
	}
}

type _GUID struct {
	data1 uint32
	data2 uint16
	data3 uint16
	data4 [8]uint8
}

type _SP_DEVICE_INTERFACE_DATA struct {
	cbSize   uint32
	guid     _GUID
	flags    uint32
	reserved uintptr
}

type _SP_DEVICE_INTERFACE_DETAIL_DATA_W struct {
	cbSize     uint32
	devicePath [1]uint16
}

type _HIDD_ATTRIBUTES struct {
	size      uint32
	vendorID  uint16
	productID uint16
	version   uint16
}

type _HIDP_CAPS struct {
	usage                     uint16
	usagePage                 uint16
	inputReportByteLength     uint16
	outputReportByteLength    uint16
	featureReportByteLength   uint16
	reserved                  [17]uint16
	numberLinkCollectionNodes uint16
	numberInputButtonCaps     uint16
	numberInputValueCaps      uint16
	numberInputDataIndices    uint16
	numberOutputButtonCaps    uint16
	numberOutputValueCaps     uint16
	numberOutputDataIndices   uint16
	numberFeatureButtonCaps   uint16
	numberFeatureValueCaps    uint16
	numberFeatureDataIndices  uint16
}

type deviceExtra struct {
	// mtx is held for reading by I/O in flight, and for writing while the
	// handles below change, so a handle is never used after it is closed.
	mtx     sync.RWMutex
	closing atomic.Bool

	file         uintptr
	evInput      uintptr
	evOutput     uintptr
	evGetFeature uintptr
	evSetFeature uintptr
}

const (
	null uintptr = 0

	_GENERIC_WRITE uint32 = 0x40000000
	_GENERIC_READ  uint32 = 0x80000000

	_OPEN_EXISTING uint32 = 0x00000003

	_FILE_FLAG_OVERLAPPED uint32 = 0x40000000

	_FILE_SHARE_READ  uint32 = 0x00000001
	_FILE_SHARE_WRITE uint32 = 0x00000002

	_IOCTL_HID_SET_FEATURE uint32 = 0x000b0191
	_IOCTL_HID_GET_FEATURE uint32 = 0x000b0192

	_INVALID_HANDLE_VALUE = ^uintptr(0)

	_WAIT_OBJECT_0 uint32 = 0x00000000
	_WAIT_TIMEOUT  uint32 = 0x00000102
	_WAIT_FAILED   uint32 = 0xffffffff

	_ERROR_SHARING_VIOLATION   syscall.Errno = 0x00000020
	_ERROR_INSUFFICIENT_BUFFER syscall.Errno = 0x0000007a
	_ERROR_NO_MORE_ITEMS       syscall.Errno = 0x00000103
	_ERROR_OPERATION_ABORTED   syscall.Errno = 0x000003e3
	_ERROR_IO_PENDING          syscall.Errno = 0x000003e5
)

// failure tells how a function reports that it failed. The last error is only
// meaningful after a failure: functions may set it when they succeed.
type failure byte

const (
	failsNever   failure = iota // nothing to check, or checked by the caller
	failsBOOL                   // zero, of which only the low 32 bits are defined
	failsBOOLEAN                // zero, of which only the low 8 bits are defined
	failsHandle                 // INVALID_HANDLE_VALUE
	failsNull                   // NULL
	failsWait                   // WAIT_FAILED
)

func (f failure) failed(r uintptr) bool {
	switch f {
	case failsBOOL:
		return uint32(r) == 0
	case failsBOOLEAN:
		return uint8(r) == 0
	case failsHandle:
		return r == _INVALID_HANDLE_VALUE
	case failsNull:
		return r == 0
	case failsWait:
		return uint32(r) == _WAIT_FAILED
	}
	return false
}

type proc struct {
	*windows.LazyProc
	failure failure
}

// newSystemDLL loads a library from the system directory only. hid.dll is not
// a known DLL, so a search that includes the application directory would load
// a copy planted next to the executable.
func newSystemDLL(name string) *windows.LazyDLL {
	return windows.NewLazySystemDLL(name)
}

func newProc(dll *windows.LazyDLL, name string, f failure) *proc {
	return &proc{dll.NewProc(name), f}
}

var (
	kernel32             = newSystemDLL("kernel32.dll")
	_CancelIoEx          = newProc(kernel32, "CancelIoEx", failsBOOL)
	_CreateEventW        = newProc(kernel32, "CreateEventW", failsNull)
	_CreateFileW         = newProc(kernel32, "CreateFileW", failsHandle)
	_CloseHandle         = newProc(kernel32, "CloseHandle", failsBOOL)
	_DeviceIoControl     = newProc(kernel32, "DeviceIoControl", failsBOOL)
	_GetOverlappedResult = newProc(kernel32, "GetOverlappedResult", failsBOOL)
	_ReadFile            = newProc(kernel32, "ReadFile", failsBOOL)
	_WaitForSingleObject = newProc(kernel32, "WaitForSingleObject", failsWait)
	_WriteFile           = newProc(kernel32, "WriteFile", failsBOOL)
)

const (
	_DIGCF_PRESENT         uint32 = 0x00000002
	_DIGCF_DEVICEINTERFACE uint32 = 0x00000010
)

var (
	setupapi                          = newSystemDLL("setupapi.dll")
	_SetupDiDestroyDeviceInfoList     = newProc(setupapi, "SetupDiDestroyDeviceInfoList", failsBOOL)
	_SetupDiEnumDeviceInterfaces      = newProc(setupapi, "SetupDiEnumDeviceInterfaces", failsBOOL)
	_SetupDiGetClassDevsW             = newProc(setupapi, "SetupDiGetClassDevsW", failsHandle)
	_SetupDiGetDeviceInterfaceDetailW = newProc(setupapi, "SetupDiGetDeviceInterfaceDetailW", failsBOOL)
)

const (
	_HIDP_STATUS_SUCCESS uint32 = 0x00110000
)

var (
	hid                         = newSystemDLL("hid.dll")
	_HidD_FreePreparsedData     = newProc(hid, "HidD_FreePreparsedData", failsBOOLEAN)
	_HidD_GetAttributes         = newProc(hid, "HidD_GetAttributes", failsBOOLEAN)
	_HidD_GetHidGuid            = newProc(hid, "HidD_GetHidGuid", failsNever)
	_HidD_GetManufacturerString = newProc(hid, "HidD_GetManufacturerString", failsBOOLEAN)
	_HidD_GetPreparsedData      = newProc(hid, "HidD_GetPreparsedData", failsBOOLEAN)
	_HidD_GetProductString      = newProc(hid, "HidD_GetProductString", failsBOOLEAN)
	_HidD_GetSerialNumberString = newProc(hid, "HidD_GetSerialNumberString", failsBOOLEAN)
	_HidP_GetCaps               = newProc(hid, "HidP_GetCaps", failsNever)
)

func errnoIs(err error, e syscall.Errno) bool {
	errno, match := err.(syscall.Errno)
	return match && errno == e
}

func call(p *proc, args ...any) (uintptr, error) {
	// the pointers are converted to uintptr outside of the Call expression, so
	// nothing else keeps them reachable while the kernel is using them.
	defer runtime.KeepAlive(args)

	var ovl *overlapped

	v := make([]uintptr, len(args))
	for i, arg := range args {
		switch arg := arg.(type) {
		case *overlapped:
			if arg == nil {
				return 0, errors.New("call: overlapped is nil")
			}
			ovl = arg
			ovl.ovl.hEvent = ovl.ev
			v[i] = uintptr(unsafe.Pointer(&ovl.ovl))
		case uint8:
			v[i] = uintptr(arg)
		case uint16:
			v[i] = uintptr(arg)
		case uint32:
			v[i] = uintptr(arg)
		case uint64:
			v[i] = uintptr(arg)
		case uint:
			v[i] = uintptr(arg)
		case int8:
			v[i] = uintptr(arg)
		case int16:
			v[i] = uintptr(arg)
		case int32:
			v[i] = uintptr(arg)
		case int64:
			v[i] = uintptr(arg)
		case int:
			v[i] = uintptr(arg)
		case unsafe.Pointer:
			v[i] = uintptr(arg)
		case uintptr:
			v[i] = arg
		default:
			return 0, fmt.Errorf("call: argument is not supported: %s", arg)
		}
	}

	r, _, err := p.Call(v...)
	if p.failure.failed(r) && (ovl == nil || !errnoIs(err, _ERROR_IO_PENDING)) {
		if errnoIs(err, 0) {
			return 0, fmt.Errorf("%s failed without an error code", p.Name)
		}
		return 0, err
	}

	if ovl != nil {
		// the functions that accept OVERLAPPED always have a file handle as first argument
		if err := ovl.wait(v[0]); err != nil {
			return 0, err
		}
	}

	return r, nil
}

// wait returns once the operation is over, as the kernel writes to the buffers
// and to the OVERLAPPED until then, whatever the outcome.
func (o *overlapped) wait(file uintptr) error {
	if o.timeout > 0 {
		rv, err := call(_WaitForSingleObject, o.ovl.hEvent, uint32(o.timeout.Milliseconds()))
		if err != nil {
			call(_CancelIoEx, file, unsafe.Pointer(&o.ovl))
			o.result(file)
			return err
		}
		if uint32(rv) == _WAIT_TIMEOUT {
			// fails if the operation has just completed, which result reports.
			call(_CancelIoEx, file, unsafe.Pointer(&o.ovl))
			err := o.result(file)
			if errnoIs(err, _ERROR_OPERATION_ABORTED) {
				return ErrDeviceTimeout
			}
			return err
		}
	}
	return o.result(file)
}

func (o *overlapped) result(file uintptr) error {
	_, err := call(_GetOverlappedResult, file, unsafe.Pointer(&o.ovl), unsafe.Pointer(&o.n), 1)
	return err
}

func ioctl(fd uintptr, ev uintptr, req uint32, in []byte, out []byte) (int, error) {
	var (
		inb  unsafe.Pointer
		outb unsafe.Pointer
		inl  uint32
		outl uint32
		rv   uint32
	)
	if len(in) > 0 {
		inb = unsafe.Pointer(&in[0])
		inl = uint32(len(in))
	}
	if len(out) > 0 {
		outb = unsafe.Pointer(&out[0])
		outl = uint32(len(out))
	}

	ovl := overlapped{ev: ev, timeout: writeTimeout}
	if _, err := call(_DeviceIoControl, fd, req, inb, inl, outb, outl, unsafe.Pointer(&rv), &ovl); err != nil {
		return 0, err
	}
	return int(ovl.n), nil
}

func reportLength(byteLength uint16) uint16 {
	if byteLength == 0 {
		return 0
	}
	return byteLength - 1
}

func newHiddAttributes() _HIDD_ATTRIBUTES {
	return _HIDD_ATTRIBUTES{size: uint32(unsafe.Sizeof(_HIDD_ATTRIBUTES{}))}
}

// validDetailSize checks a size for SP_DEVICE_INTERFACE_DETAIL_DATA_W returned
// by setupapi: the 4 bytes of cbSize, a path of at least one character, and its
// terminator.
func validDetailSize(size uintptr) bool {
	return size >= 8
}

func enumerate() ([]*Device, error) {
	guid := _GUID{}
	if _, err := call(_HidD_GetHidGuid, unsafe.Pointer(&guid)); err != nil {
		return nil, err
	}

	devInfo, err := call(_SetupDiGetClassDevsW, unsafe.Pointer(&guid), null, null, _DIGCF_PRESENT|_DIGCF_DEVICEINTERFACE)
	if err != nil {
		return nil, err
	}
	defer call(_SetupDiDestroyDeviceInfoList, devInfo)

	idx := uint32(0)
	rv := []*Device{}

	for {
		itf := _SP_DEVICE_INTERFACE_DATA{}
		itf.cbSize = uint32(unsafe.Sizeof(itf))

		_, err := call(_SetupDiEnumDeviceInterfaces, devInfo, null, unsafe.Pointer(&guid), idx, unsafe.Pointer(&itf))
		idx++
		if errnoIs(err, _ERROR_NO_MORE_ITEMS) {
			break
		}
		if err != nil {
			return nil, err
		}

		reqSize := uintptr(0)
		if _, err := call(_SetupDiGetDeviceInterfaceDetailW, devInfo, unsafe.Pointer(&itf), null, 0, unsafe.Pointer(&reqSize), null); !errnoIs(err, _ERROR_INSUFFICIENT_BUFFER) {
			continue
		}
		if !validDetailSize(reqSize) {
			continue
		}

		detailBuf := make([]uint16, reqSize/unsafe.Sizeof(uint16(0)))
		detail := (*_SP_DEVICE_INTERFACE_DETAIL_DATA_W)(unsafe.Pointer(&detailBuf[0]))
		// the sdk packs this structure to 1 byte on 32 bit windows, so the
		// trailing padding go adds for alignment is not part of the size that
		// setupapi validates.
		detail.cbSize = 8
		if unsafe.Sizeof(uintptr(0)) == 4 {
			detail.cbSize = 6
		}

		if _, err := call(_SetupDiGetDeviceInterfaceDetailW, devInfo, unsafe.Pointer(&itf), unsafe.Pointer(detail), reqSize, null, null); err != nil {
			continue
		}

		pathW := detailBuf[unsafe.Offsetof(detail.devicePath)/unsafe.Sizeof(detailBuf[0]) : len(detailBuf)-1]

		d := func() *Device {
			f, err := call(_CreateFileW, unsafe.Pointer(&pathW[0]), 0, _FILE_SHARE_READ|_FILE_SHARE_WRITE, null, _OPEN_EXISTING, 0, null)
			if err != nil {
				return nil
			}
			defer call(_CloseHandle, f)

			attr := newHiddAttributes()
			if _, err := call(_HidD_GetAttributes, f, unsafe.Pointer(&attr)); err != nil {
				return nil
			}

			rv := &Device{
				path:      syscall.UTF16ToString(pathW),
				vendorId:  attr.vendorID,
				productId: attr.productID,
				version:   attr.version,
			}
			buf := make([]uint16, 4092/unsafe.Sizeof(uint16(0)))
			if _, err := call(_HidD_GetManufacturerString, f, unsafe.Pointer(&buf[0]), len(buf)); err == nil {
				rv.manufacturer = syscall.UTF16ToString(buf)
			}

			if _, err := call(_HidD_GetProductString, f, unsafe.Pointer(&buf[0]), len(buf)); err == nil {
				rv.product = syscall.UTF16ToString(buf)
			}

			if _, err := call(_HidD_GetSerialNumberString, f, unsafe.Pointer(&buf[0]), len(buf)); err == nil {
				rv.serialNumber = syscall.UTF16ToString(buf)
			}

			var preparsed uintptr
			if _, err := call(_HidD_GetPreparsedData, f, unsafe.Pointer(&preparsed)); err != nil {
				return nil
			}
			defer call(_HidD_FreePreparsedData, preparsed)

			var caps _HIDP_CAPS
			if status, err := call(_HidP_GetCaps, preparsed, unsafe.Pointer(&caps)); err != nil || uint32(status) != _HIDP_STATUS_SUCCESS {
				return nil
			}

			rv.usagePage = caps.usagePage
			rv.usage = caps.usage
			rv.reportInputLength = reportLength(caps.inputReportByteLength)
			rv.reportOutputLength = reportLength(caps.outputReportByteLength)
			rv.reportFeatureLength = reportLength(caps.featureReportByteLength)
			rv.reportWithId = true
			return rv
		}()

		if d != nil {
			rv = append(rv, d)
		}
	}

	return rv, nil
}

func (d *Device) open(lock bool) error {
	pathW, err := syscall.UTF16FromString(d.path)
	if err != nil {
		return err
	}

	share := _FILE_SHARE_READ | _FILE_SHARE_WRITE
	if lock {
		share = 0
	}

	f, err := call(_CreateFileW, unsafe.Pointer(&pathW[0]), _GENERIC_READ|_GENERIC_WRITE, share, null, _OPEN_EXISTING, _FILE_FLAG_OVERLAPPED, null)
	if err != nil {
		if errnoIs(err, _ERROR_SHARING_VIOLATION) {
			return ErrDeviceLocked
		}
		return err
	}

	handles := []uintptr{f}
	release := func() {
		for _, h := range handles {
			call(_CloseHandle, h)
		}
	}

	// manual reset, as required for an event that is waited on separately from
	// GetOverlappedResult.
	events := [4]uintptr{}
	for i := range events {
		h, err := call(_CreateEventW, null, 1, 0, null)
		if err != nil {
			release()
			return err
		}
		events[i] = h
		handles = append(handles, h)
	}

	// the handles are only visible to other calls when all of them exist.
	d.extra.mtx.Lock()
	defer d.extra.mtx.Unlock()

	if d.extra.file != null {
		release()
		return ErrDeviceIsOpen
	}

	d.extra.file = f
	d.extra.evInput = events[0]
	d.extra.evOutput = events[1]
	d.extra.evGetFeature = events[2]
	d.extra.evSetFeature = events[3]
	return nil
}

func (d *Device) isOpen() bool {
	d.extra.mtx.RLock()
	defer d.extra.mtx.RUnlock()
	return d.extra.file != null
}

func (d *Device) close() error {
	if !d.extra.closing.CompareAndSwap(false, true) {
		return ErrDeviceIsClosed
	}
	defer d.extra.closing.Store(false)

	d.extra.mtx.RLock()
	file := d.extra.file
	d.extra.mtx.RUnlock()
	if file == null {
		return ErrDeviceIsClosed
	}

	// an operation can start right after a cancellation, so keep cancelling
	// until none is in flight. the handle can't be closed before that, as it
	// and the events are in use by those operations.
	for !d.extra.mtx.TryLock() {
		call(_CancelIoEx, file, null)
		time.Sleep(time.Millisecond)
	}
	defer d.extra.mtx.Unlock()

	var firstErr error
	for _, h := range []*uintptr{&d.extra.file, &d.extra.evInput, &d.extra.evOutput, &d.extra.evGetFeature, &d.extra.evSetFeature} {
		if *h == null {
			continue
		}
		if _, err := call(_CloseHandle, *h); err != nil && firstErr == nil {
			firstErr = err
		}
		*h = null
	}
	return firstErr
}

// beginIO returns the handles to use for an operation, which stay valid until
// the matching endIO.
func (d *Device) beginIO(ev *uintptr) (uintptr, uintptr, error) {
	d.extra.mtx.RLock()
	if d.extra.file == null || d.extra.closing.Load() {
		d.extra.mtx.RUnlock()
		return null, null, ErrDeviceIsClosed
	}
	return d.extra.file, *ev, nil
}

func (d *Device) endIO(err error) error {
	closing := d.extra.closing.Load()
	d.extra.mtx.RUnlock()
	if closing && errnoIs(err, _ERROR_OPERATION_ABORTED) {
		return ErrDeviceIsClosed
	}
	return err
}

func (d *Device) getInputReport() (byte, []byte, error) {
	file, ev, err := d.beginIO(&d.extra.evInput)
	if err != nil {
		return 0, nil, err
	}

	ovl := overlapped{ev: ev}
	buf := make([]byte, int(d.reportInputLength)+1)
	_, err = call(_ReadFile, file, unsafe.Pointer(&buf[0]), len(buf), 0, &ovl)
	if err := d.endIO(err); err != nil {
		return 0, nil, err
	}
	if ovl.n < 1 || ovl.n > uint32(len(buf)) {
		return 0, nil, fmt.Errorf("invalid report length: %d", ovl.n)
	}
	return buf[0], buf[1:ovl.n], nil
}

func (d *Device) setOutputReport(reportId byte, data []byte) error {
	file, ev, err := d.beginIO(&d.extra.evOutput)
	if err != nil {
		return err
	}

	// windows expects a buffer of the full report length, even when the report
	// for this id is shorter.
	buf := make([]byte, int(d.reportOutputLength)+1)
	buf[0] = reportId
	copy(buf[1:], data)

	ovl := overlapped{ev: ev, timeout: writeTimeout}
	_, err = call(_WriteFile, file, unsafe.Pointer(&buf[0]), len(buf), 0, &ovl)
	if err := d.endIO(err); err != nil {
		return err
	}
	return checkWritten(int(ovl.n), len(buf))
}

func checkWritten(written int, want int) error {
	if written != want {
		return fmt.Errorf("short write: %d of %d bytes", written, want)
	}
	return nil
}

func (d *Device) getFeatureReport(reportId byte) ([]byte, error) {
	file, ev, err := d.beginIO(&d.extra.evGetFeature)
	if err != nil {
		return nil, err
	}

	buf := make([]byte, int(d.reportFeatureLength)+1)
	buf[0] = reportId

	n, err := ioctl(file, ev, _IOCTL_HID_GET_FEATURE, nil, buf)
	if err := d.endIO(err); err != nil {
		return nil, err
	}
	if n < 1 || n > len(buf) {
		return nil, fmt.Errorf("invalid report length: %d", n)
	}
	return buf[1:n], nil
}

func (d *Device) setFeatureReport(reportId byte, data []byte) error {
	file, ev, err := d.beginIO(&d.extra.evSetFeature)
	if err != nil {
		return err
	}

	// windows expects a buffer of the full report length, even when the report
	// for this id is shorter.
	buf := make([]byte, int(d.reportFeatureLength)+1)
	buf[0] = reportId
	copy(buf[1:], data)

	_, err = ioctl(file, ev, _IOCTL_HID_SET_FEATURE, buf, nil)
	return d.endIO(err)
}
