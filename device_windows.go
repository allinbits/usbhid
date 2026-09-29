// Copyright 2022-2024 Rafael G. Martins. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package usbhid

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

type overlapped struct {
	n   uint32
	ev  uintptr
	ovl struct {
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

	_ERROR_SUCCESS             syscall.Errno = 0x00000000
	_ERROR_SHARING_VIOLATION   syscall.Errno = 0x00000020
	_ERROR_INSUFFICIENT_BUFFER syscall.Errno = 0x0000007a
	_ERROR_IO_PENDING          syscall.Errno = 0x000003e5
)

var (
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	_CreateEventW        = kernel32.NewProc("CreateEventW")
	_CreateFileW         = kernel32.NewProc("CreateFileW")
	_CloseHandle         = kernel32.NewProc("CloseHandle")
	_DeviceIoControl     = kernel32.NewProc("DeviceIoControl")
	_GetOverlappedResult = kernel32.NewProc("GetOverlappedResult")
	_ReadFile            = kernel32.NewProc("ReadFile")
	_WriteFile           = kernel32.NewProc("WriteFile")
)

const (
	_DIGCF_PRESENT         uint32 = 0x00000002
	_DIGCF_DEVICEINTERFACE uint32 = 0x00000010
)

var (
	setupapi                          = syscall.NewLazyDLL("setupapi.dll")
	_SetupDiDestroyDeviceInfoList     = setupapi.NewProc("SetupDiDestroyDeviceInfoList")
	_SetupDiEnumDeviceInterfaces      = setupapi.NewProc("SetupDiEnumDeviceInterfaces")
	_SetupDiGetClassDevsW             = setupapi.NewProc("SetupDiGetClassDevsW")
	_SetupDiGetDeviceInterfaceDetailW = setupapi.NewProc("SetupDiGetDeviceInterfaceDetailW")
)

const (
	_HIDP_STATUS_SUCCESS uint32 = 0x00110000
)

var (
	hid                         = syscall.NewLazyDLL("hid.dll")
	_HidD_FreePreparsedData     = hid.NewProc("HidD_FreePreparsedData")
	_HidD_GetAttributes         = hid.NewProc("HidD_GetAttributes")
	_HidD_GetHidGuid            = hid.NewProc("HidD_GetHidGuid")
	_HidD_GetManufacturerString = hid.NewProc("HidD_GetManufacturerString")
	_HidD_GetPreparsedData      = hid.NewProc("HidD_GetPreparsedData")
	_HidD_GetProductString      = hid.NewProc("HidD_GetProductString")
	_HidD_GetSerialNumberString = hid.NewProc("HidD_GetSerialNumberString")
	_HidP_GetCaps               = hid.NewProc("HidP_GetCaps")
)

func errnoIs(err error, e syscall.Errno) bool {
	errno, match := err.(syscall.Errno)
	return match && errno == e
}

func callError(err error, accept ...syscall.Errno) error {
	errno, match := err.(syscall.Errno)
	if !match {
		return err
	}
	if errno == _ERROR_SUCCESS {
		return nil
	}
	for _, a := range accept {
		if errno == a {
			return nil
		}
	}
	return errno
}

func call(p *syscall.LazyProc, args ...any) (uintptr, error) {
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
			ev := ovl.ev
			if ev == null {
				var err error
				if ev, err = call(_CreateEventW, null, 0, 0, null); err != nil {
					return 0, err
				}
				defer call(_CloseHandle, ev)
			}
			ovl.ovl.hEvent = ev
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
	if ovl == nil {
		err = callError(err)
	} else {
		err = callError(err, _ERROR_IO_PENDING)
	}
	if err != nil {
		return 0, err
	}

	if ovl != nil {
		// the functions that accept OVERLAPPED always have a file handle as first argument
		_, _, err := _GetOverlappedResult.Call(v[0], uintptr(unsafe.Pointer(&ovl.ovl)), uintptr(unsafe.Pointer(&ovl.n)), 1)
		if err := callError(err); err != nil {
			return 0, err
		}
	}

	return r, nil
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

	ovl := overlapped{ev: ev}
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

		b, err := call(_SetupDiEnumDeviceInterfaces, devInfo, null, unsafe.Pointer(&guid), idx, unsafe.Pointer(&itf))
		idx++
		if b == 0 {
			break
		}
		if err != nil {
			continue
		}

		reqSize := uintptr(0)
		if _, err := call(_SetupDiGetDeviceInterfaceDetailW, devInfo, unsafe.Pointer(&itf), null, 0, unsafe.Pointer(&reqSize), null); err != nil {
			if !errnoIs(err, _ERROR_INSUFFICIENT_BUFFER) {
				continue
			}
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

			attr := _HIDD_ATTRIBUTES{}
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
			if b, err := call(_HidD_GetPreparsedData, f, unsafe.Pointer(&preparsed)); err != nil || b == 0 {
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
	d.extra.file = f

	for _, ev := range []*uintptr{&d.extra.evInput, &d.extra.evOutput, &d.extra.evGetFeature, &d.extra.evSetFeature} {
		h, err := call(_CreateEventW, null, 0, 0, null)
		if err != nil {
			d.close()
			return err
		}
		*ev = h
	}
	return nil
}

func (d *Device) isOpen() bool {
	return d.extra.file != null
}

func (d *Device) close() error {
	if _, err := call(_CloseHandle, d.extra.file); err != nil {
		return err
	}
	d.extra.file = null

	for _, ev := range []*uintptr{&d.extra.evInput, &d.extra.evOutput, &d.extra.evGetFeature, &d.extra.evSetFeature} {
		if *ev == null {
			continue
		}
		if _, err := call(_CloseHandle, *ev); err != nil {
			return err
		}
		*ev = null
	}
	return nil
}

func (d *Device) getInputReport() (byte, []byte, error) {
	ovl := overlapped{ev: d.extra.evInput}
	buf := make([]byte, d.reportInputLength+1)
	if _, err := call(_ReadFile, d.extra.file, unsafe.Pointer(&buf[0]), len(buf), 0, &ovl); err != nil {
		return 0, nil, err
	}
	if ovl.n < 1 || ovl.n > uint32(len(buf)) {
		return 0, nil, fmt.Errorf("invalid report length: %d", ovl.n)
	}
	return buf[0], buf[1:ovl.n], nil
}

func (d *Device) setOutputReport(reportId byte, data []byte) error {
	// windows expects a buffer of the full report length, even when the report
	// for this id is shorter.
	buf := make([]byte, d.reportOutputLength+1)
	buf[0] = reportId
	copy(buf[1:], data)

	_, err := call(_WriteFile, d.extra.file, unsafe.Pointer(&buf[0]), len(buf), 0, &overlapped{ev: d.extra.evOutput})
	return err
}

func (d *Device) getFeatureReport(reportId byte) ([]byte, error) {
	buf := make([]byte, d.reportFeatureLength+1)
	buf[0] = reportId

	n, err := ioctl(d.extra.file, d.extra.evGetFeature, _IOCTL_HID_GET_FEATURE, nil, buf)
	if err != nil {
		return nil, err
	}
	if n < 1 || n > len(buf) {
		return nil, fmt.Errorf("invalid report length: %d", n)
	}
	return buf[1:n], nil
}

func (d *Device) setFeatureReport(reportId byte, data []byte) error {
	// windows expects a buffer of the full report length, even when the report
	// for this id is shorter.
	buf := make([]byte, d.reportFeatureLength+1)
	buf[0] = reportId
	copy(buf[1:], data)

	_, err := ioctl(d.extra.file, d.extra.evSetFeature, _IOCTL_HID_SET_FEATURE, buf, nil)
	return err
}
