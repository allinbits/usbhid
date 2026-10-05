// Copyright 2022-2024 Rafael G. Martins. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package usbhid

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

type deviceExtra struct {
	file *os.File
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

func ioctl(fd uintptr, request uint, arg uintptr) (int, error) {
	rv, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(request), arg)
	if errno != 0 {
		return 0, fmt.Errorf("ioctl failed: 0x%x: %s", request, errno)
	}
	return int(rv), nil
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

	flag := syscall.LOCK_SH
	if lock {
		flag = syscall.LOCK_EX
	}
	if err := syscall.Flock(int(f.Fd()), flag|syscall.LOCK_NB); err != nil {
		f.Close()
		if err == syscall.EWOULDBLOCK {
			return ErrDeviceLocked
		}
		return err
	}

	d.extra.file = f
	return nil
}

func (d *Device) isOpen() bool {
	return d.extra.file != nil
}

func (d *Device) close() error {
	if d.extra.file == nil {
		return nil
	}

	if err := d.extra.file.Close(); err != nil {
		return err
	}
	d.extra.file = nil
	return nil
}

func (d *Device) getInputReport() (byte, []byte, error) {
	buflen := d.reportInputLength
	if d.reportWithId {
		buflen++
	}

	buf := make([]byte, buflen)

	n, err := d.extra.file.Read(buf)
	if err != nil {
		return 0, nil, err
	}

	if d.reportWithId {
		return buf[0], buf[1:n], nil
	}
	return 0, buf[:n], nil
}

func (d *Device) setOutputReport(reportId byte, data []byte) error {
	buf := append([]byte{reportId}, data...)
	_, err := d.extra.file.Write(buf)
	return err
}

func (d *Device) getFeatureReport(reportId byte) ([]byte, error) {
	buf := make([]byte, d.reportFeatureLength+1)
	if d.reportWithId {
		buf[0] = reportId
	}

	rv, err := ioctl(d.extra.file.Fd(), uint(ioc(iocWrite|iocRead, 'H', 0x07, uint16(len(buf)))), uintptr(unsafe.Pointer(&buf[0])))
	if err != nil {
		return nil, err
	}

	// hidraw always stores the report number in the first byte and counts it
	// in the returned length, even for descriptors without report ids.
	if rv < 1 || rv > len(buf) {
		return nil, fmt.Errorf("invalid report length: %d", rv)
	}
	return buf[1:rv], nil
}

func (d *Device) setFeatureReport(reportId byte, data []byte) error {
	buf := append([]byte{reportId}, data...)
	_, err := ioctl(d.extra.file.Fd(), uint(ioc(iocWrite|iocRead, 'H', 0x06, uint16(len(buf)))), uintptr(unsafe.Pointer(&buf[0])))
	return err
}
