// Copyright 2022-2024 Rafael G. Martins. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package usbhid

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

type deviceExtra struct {
	// mtx only guards the pointer. os.File is safe for concurrent use, and
	// closing it makes operations in flight return, so nothing is held during
	// I/O.
	mtx  sync.RWMutex
	file *os.File
}

// hidrawDevinfo is struct hidraw_devinfo.
type hidrawDevinfo struct {
	bustype uint32
	vendor  int16
	product int16
}

const (
	iocNrShift   = 0
	iocTypeShift = 8
	iocSizeShift = 16
)

var (
	iocWrite    byte
	iocRead     byte
	iocSizeBits byte
	iocDirBits  byte

	iocDirShift byte
)

func init() {
	switch runtime.GOARCH {
	case "386", "amd64", "arm", "arm64", "loong64", "riscv64", "s390x":
		iocWrite = 1
		iocRead = 2
		iocSizeBits = 14
		iocDirBits = 2

	case "mips", "mips64", "mips64le", "mipsle", "ppc", "ppc64", "ppc64le", "sparc64":
		iocWrite = 4
		iocRead = 2
		iocSizeBits = 13
		iocDirBits = 3

	default:
		panic("usbhid: unsupported architecture")
	}

	iocDirShift = iocSizeShift + iocSizeBits
}

func ioc(dir byte, typ byte, nr byte, size uint16) uint32 {
	dir = dir & (1<<iocDirBits - 1)
	size = size & (1<<iocSizeBits - 1)
	return uint32(dir)<<iocDirShift | uint32(typ)<<iocTypeShift | uint32(nr)<<iocNrShift | uint32(size)<<iocSizeShift
}

// ioctl takes the argument as a pointer so that it is converted to uintptr in
// the call to syscall.Syscall, which is what keeps it alive and in place during
// the call.
var ioctl = func(fd uintptr, request uint, arg unsafe.Pointer) (int, error) {
	for {
		rv, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(request), uintptr(arg))
		if errno == syscall.EINTR {
			continue
		}
		if errno != 0 {
			return 0, fmt.Errorf("ioctl failed: 0x%x: %w", request, errno)
		}
		return int(rv), nil
	}
}

// control runs fn with the file descriptor. Unlike File.Fd, it leaves the file
// in non-blocking mode and registered with the runtime poller, which is what
// allows Close to interrupt a read.
func control(f *os.File, fn func(fd uintptr) error) error {
	conn, err := f.SyscallConn()
	if err != nil {
		return err
	}

	var fnErr error
	if err := conn.Control(func(fd uintptr) { fnErr = fn(fd) }); err != nil {
		return err
	}
	return fnErr
}

var hidrawGetInfo = func(f *os.File) (hidrawDevinfo, error) {
	var info hidrawDevinfo
	err := control(f, func(fd uintptr) error {
		_, err := ioctl(fd, uint(ioc(iocRead, 'H', 0x03, uint16(unsafe.Sizeof(info)))), unsafe.Pointer(&info))
		return err
	})
	return info, err
}

// featureRequest encodes a feature report ioctl for a buffer of the given
// length, which has to fit the size field of the request.
func featureRequest(nr byte, length int) (uint, error) {
	if length >= 1<<iocSizeBits {
		return 0, fmt.Errorf("report too long for ioctl: %d bytes", length)
	}
	return uint(ioc(iocWrite|iocRead, 'H', nr, uint16(length))), nil
}

func sysfsReadAsBytes(dir string, entry string) ([]byte, error) {
	return os.ReadFile(filepath.Join(dir, entry))
}

func sysfsReadAsString(dir string, entry string) (string, error) {
	b, err := sysfsReadAsBytes(dir, entry)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func sysfsReadAsUint(dir string, entry string, base int, bitSize int) (uint64, error) {
	v, err := sysfsReadAsString(dir, entry)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(v, base, bitSize)
}

func sysfsReadAsHexUint16(dir string, entry string) (uint16, error) {
	v, err := sysfsReadAsUint(dir, entry, 16, 16)
	return uint16(v), err
}

func enumerate() ([]*Device, error) {
	rv := []*Device{}

	root := "/sys/bus/usb/devices"
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// a single unreadable entry must not abort the enumeration.
			if path == root {
				return err
			}
			return nil
		}

		if info.Mode()&os.ModeSymlink == 0 || strings.Contains(info.Name(), ":") {
			return nil
		}

		files, err := filepath.Glob(filepath.Join(path, "[0-9]*", "[0-9]*", "hidraw", "hidraw[0-9]*"))
		if err != nil || len(files) == 0 {
			return nil
		}

		vendorId, err := sysfsReadAsHexUint16(path, "idVendor")
		if err != nil {
			return nil
		}

		productId, err := sysfsReadAsHexUint16(path, "idProduct")
		if err != nil {
			return nil
		}

		version, err := sysfsReadAsHexUint16(path, "bcdDevice")
		if err != nil {
			return nil
		}

		var manufacturer string
		if m, err := sysfsReadAsString(path, "manufacturer"); err == nil {
			manufacturer = m
		}

		var product string
		if p, err := sysfsReadAsString(path, "product"); err == nil {
			product = p
		}

		var serialNumber string
		if s, err := sysfsReadAsString(path, "serial"); err == nil {
			serialNumber = s
		}

		for _, f := range files {
			hidpath := filepath.Dir(filepath.Dir(f))
			descriptor, err := sysfsReadAsBytes(hidpath, "report_descriptor")
			if err != nil {
				continue
			}

			d := &Device{
				path:         filepath.Join("/dev", filepath.Base(f)),
				vendorId:     vendorId,
				productId:    productId,
				version:      version,
				manufacturer: manufacturer,
				product:      product,
				serialNumber: serialNumber,
			}
			d.usagePage, d.usage, d.reportInputLength, d.reportOutputLength, d.reportFeatureLength, d.reportWithId = hidParseReportDescriptor(descriptor)

			rv = append(rv, d)
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return rv, nil
}

func (d *Device) open(lock bool) error {
	f, err := os.OpenFile(d.path, os.O_RDWR, 0755)
	if err != nil {
		return err
	}

	if err := d.prepare(f, lock); err != nil {
		f.Close()
		return err
	}

	d.extra.mtx.Lock()
	defer d.extra.mtx.Unlock()

	if d.extra.file != nil {
		f.Close()
		return ErrDeviceIsOpen
	}
	d.extra.file = f
	return nil
}

func (d *Device) prepare(f *os.File, lock bool) error {
	flag := syscall.LOCK_SH
	if lock {
		flag = syscall.LOCK_EX
	}
	if err := control(f, func(fd uintptr) error {
		return syscall.Flock(int(fd), flag|syscall.LOCK_NB)
	}); err != nil {
		if err == syscall.EWOULDBLOCK {
			return ErrDeviceLocked
		}
		return err
	}

	// the kernel reuses hidraw minors: the device may have been replaced at
	// this path since it was enumerated.
	info, err := hidrawGetInfo(f)
	if err != nil {
		return err
	}
	if uint16(info.vendor) != d.vendorId || uint16(info.product) != d.productId {
		return fmt.Errorf("%s is now a different device: vid=0x%04x; pid=0x%04x", d.path, uint16(info.vendor), uint16(info.product))
	}
	return nil
}

func (d *Device) handle() *os.File {
	d.extra.mtx.RLock()
	defer d.extra.mtx.RUnlock()
	return d.extra.file
}

func (d *Device) isOpen() bool {
	return d.handle() != nil
}

func (d *Device) close() error {
	d.extra.mtx.Lock()
	f := d.extra.file
	d.extra.file = nil
	d.extra.mtx.Unlock()

	if f == nil {
		return ErrDeviceIsClosed
	}
	return f.Close()
}

// ioError translates the errors that have a meaning of their own. Errors from
// RawConn.Control don't match os.ErrClosed, hence the check of the state.
func (d *Device) ioError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, os.ErrClosed) || !d.isOpen():
		return ErrDeviceIsClosed
	case errors.Is(err, syscall.ETIMEDOUT):
		return ErrDeviceTimeout
	}
	return err
}

func (d *Device) getInputReport() (byte, []byte, error) {
	f := d.handle()
	if f == nil {
		return 0, nil, ErrDeviceIsClosed
	}

	buflen := int(d.reportInputLength)
	if d.reportWithId {
		buflen++
	}

	buf := make([]byte, buflen)

	n, err := f.Read(buf)
	if err != nil {
		return 0, nil, d.ioError(err)
	}

	if d.reportWithId {
		if n < 1 {
			return 0, nil, fmt.Errorf("invalid report length: %d", n)
		}
		return buf[0], buf[1:n], nil
	}
	return 0, buf[:n], nil
}

func (d *Device) setOutputReport(reportId byte, data []byte) error {
	f := d.handle()
	if f == nil {
		return ErrDeviceIsClosed
	}

	buf := append([]byte{reportId}, data...)
	_, err := f.Write(buf)
	return d.ioError(err)
}

func (d *Device) getFeatureReport(reportId byte) ([]byte, error) {
	f := d.handle()
	if f == nil {
		return nil, ErrDeviceIsClosed
	}

	buf := make([]byte, int(d.reportFeatureLength)+1)
	if d.reportWithId {
		buf[0] = reportId
	}

	req, err := featureRequest(0x07, len(buf))
	if err != nil {
		return nil, err
	}

	var rv int
	if err := control(f, func(fd uintptr) (err error) {
		rv, err = ioctl(fd, req, unsafe.Pointer(&buf[0]))
		return err
	}); err != nil {
		return nil, d.ioError(err)
	}

	// hidraw always stores the report number in the first byte and counts it
	// in the returned length, even for descriptors without report ids.
	if rv < 1 || rv > len(buf) {
		return nil, fmt.Errorf("invalid report length: %d", rv)
	}
	return buf[1:rv], nil
}

func (d *Device) setFeatureReport(reportId byte, data []byte) error {
	f := d.handle()
	if f == nil {
		return ErrDeviceIsClosed
	}

	buf := append([]byte{reportId}, data...)

	req, err := featureRequest(0x06, len(buf))
	if err != nil {
		return err
	}

	var written int
	if err := control(f, func(fd uintptr) (err error) {
		written, err = ioctl(fd, req, unsafe.Pointer(&buf[0]))
		return err
	}); err != nil {
		return d.ioError(err)
	}

	// the count includes the report number, even for descriptors without
	// report ids.
	if written != len(buf) {
		return fmt.Errorf("short write: %d of %d bytes", written, len(buf))
	}
	return nil
}
