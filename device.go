// Copyright 2022-2024 Rafael G. Martins. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package usbhid provides support for interacting with USB HID
// devices connected to a computer, from userspace.
//
// It is written in pure Go and works on Linux, macOS and Windows.
//
// # Concurrency
//
// Close may be called while other operations are in flight. A pending
// GetInputReport returns an error matching ErrDeviceIsClosed, which makes Close
// the way to cancel it, as it otherwise blocks until the device sends a report.
// A write or feature report in flight may instead finish with its own result.
// Operations started after Close fail with ErrDeviceIsClosed. Reads and writes
// may run in different goroutines. Calls of the same kind, such as two concurrent
// GetInputReport calls, must not overlap. Open must not overlap any other call
// on the same Device.
package usbhid

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

// Errors returned from usbhid package may be tested against these errors
// with errors.Is.
var (
	ErrDeviceEnumerationFailed = errors.New("usb hid device enumeration failed")
	ErrDeviceFailedToClose     = errors.New("usb hid device failed to close")
	ErrDeviceFailedToOpen      = errors.New("usb hid device failed to open")
	ErrDeviceIsClosed          = errors.New("usb hid device is closed")
	ErrDeviceIsOpen            = errors.New("usb hid device is open")
	ErrDeviceLocked            = errors.New("usb hid device is locked by another application")
	ErrDeviceTimeout           = errors.New("usb hid device timed out")
	ErrGetFeatureReportFailed  = errors.New("get usb hid feature report failed")
	ErrGetInputReportFailed    = errors.New("get usb hid input report failed")
	ErrMoreThanOneDeviceFound  = errors.New("more than one usb hid device found")
	ErrNoDeviceFound           = errors.New("no usb hid device found")
	ErrReportBufferOverflow    = errors.New("usb hid report buffer overflow")
	ErrSetFeatureReportFailed  = errors.New("set usb hid feature report failed")
	ErrSetOutputReportFailed   = errors.New("set usb hid output report failed")
)

// enumerateDevices is a variable for tests to replace.
var enumerateDevices = enumerate

// writeTimeout bounds how long a write or a feature report may take. A device
// that stops answering would otherwise hang its caller forever. Reads have no
// such limit, as a device may legitimately wait for its user: Close cancels them.
var writeTimeout = 5 * time.Second

// Device is an opaque structure that represents a USB HID device connected
// to the computer.
type Device struct {
	path         string
	vendorId     uint16
	productId    uint16
	version      uint16
	manufacturer string
	product      string
	serialNumber string

	usagePage           uint16
	usage               uint16
	reportInputLength   uint16
	reportOutputLength  uint16
	reportFeatureLength uint16
	reportWithId        bool

	extra deviceExtra
}

// DeviceFilterFunc is a function prototype that helps defining a filter
// function to be used by the device enumeration functions.
type DeviceFilterFunc func(*Device) bool

// Enumerate lists the USB HID devices connected to the computer, optionally
// filtered by a DeviceFilterFunc function. The devices are sorted by path, so
// the order is the same between calls for as long as the same devices are
// connected. It says nothing about which device was connected first.
func Enumerate(f DeviceFilterFunc) ([]*Device, error) {
	devices, err := enumerateDevices()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDeviceEnumerationFailed, err)
	}

	sort.Slice(devices, func(i, j int) bool {
		return devices[i].path < devices[j].path
	})

	if f == nil {
		return devices, nil
	}

	rv := []*Device{}
	for _, dev := range devices {
		if f(dev) {
			rv = append(rv, dev)
		}
	}
	return rv, nil
}

// Get returns a USB HID device found connected to the machine that matches the
// DeviceFilterfunc function. It can optionally open the device and acquire an
// exclusive lock.
//
// If the filtering would result in more than one device, or zero devices, an
// error is returned.
func Get(f DeviceFilterFunc, open bool, lock bool) (*Device, error) {
	devices, err := Enumerate(f)
	if err != nil {
		return nil, err
	}

	if l := len(devices); l == 0 {
		return nil, ErrNoDeviceFound
	} else if l > 1 {
		return nil, ErrMoreThanOneDeviceFound
	}

	d := devices[0]

	if open {
		if err := d.Open(lock); err != nil {
			return nil, err
		}
	}

	return d, nil
}

// String returns a platform-independent string representation of the device.
func (d *Device) String() string {
	rv := fmt.Sprintf("vid=0x%04x; pid=0x%04x", d.vendorId, d.productId)
	if d.manufacturer != "" {
		rv += fmt.Sprintf("; mfr=%q", d.manufacturer)
	}
	if d.product != "" {
		rv += fmt.Sprintf("; prod=%q", d.product)
	}
	if d.serialNumber != "" {
		rv += fmt.Sprintf("; sn=%q", d.serialNumber)
	}
	return rv
}

// Open opens the USB HID device for usage.
//
// If lock is true, the device is opened for exclusive access. macOS and Windows
// enforce it against every other application. On Linux it is an advisory
// flock(2), which only applications taking the same lock respect: hidapi users
// such as Ledger Wallet ignore it, and then both applications receive every
// input report.
func (d *Device) Open(lock bool) error {
	if d.isOpen() {
		return fmt.Errorf("%w [%s]", ErrDeviceIsOpen, d)
	}

	if err := d.open(lock); err != nil {
		if err == ErrDeviceIsOpen {
			return fmt.Errorf("%w [%s]", ErrDeviceIsOpen, d)
		}
		if err == ErrDeviceLocked {
			return fmt.Errorf("%w [%s]", ErrDeviceLocked, d)
		}
		return fmt.Errorf("%w [%s]: %w", ErrDeviceFailedToOpen, d, err)
	}
	return nil
}

// IsOpen checks if the USB HID device is open and available for usage
func (d *Device) IsOpen() bool {
	return d.isOpen()
}

// Close closes the USB HID device, and makes a pending GetInputReport return
// ErrDeviceIsClosed. A write or feature report in flight may finish with its
// own result. On macOS Close waits for a write in flight to end first, for up
// to about 6 seconds.
func (d *Device) Close() error {
	if !d.isOpen() {
		return fmt.Errorf("%w [%s]", ErrDeviceIsClosed, d)
	}

	if err := d.close(); err != nil {
		return fmt.Errorf("%w [%s]: %w", ErrDeviceFailedToClose, d, err)
	}
	return nil
}

// GetInputReport reads an input report from the USB HID device.
// It will block until a report is available, and returns the report id,
// a slice of bytes with the report content, and an error (or nil). There is no
// timeout: use Close from another goroutine to cancel it.
//
// Reports that arrive between calls are queued by the operating system, and
// are lost when the queue overflows: new reports are dropped on Linux and
// macOS (64 reports), the oldest ones on Windows (32 by default).
func (d *Device) GetInputReport() (byte, []byte, error) {
	if !d.isOpen() {
		return 0, nil, fmt.Errorf("%w [%s]", ErrDeviceIsClosed, d)
	}

	if d.reportInputLength == 0 {
		return 0, nil, fmt.Errorf("%w [%s]: device has no input reports", ErrGetInputReportFailed, d)
	}

	id, buf, err := d.getInputReport()
	if err != nil {
		return 0, nil, fmt.Errorf("%w [%s]: %w", ErrGetInputReportFailed, d, err)
	}
	return id, buf, nil
}

// SetOutputReport writes an output report to the USB HID device.
// It takes the report id and a slice of bytes with the data to be sent,
// and returns an error or nil. If the slice is bigger than the expected
// report size, an error is returned. Any padding required by the operating
// system is handled internally. It fails with an error matching
// ErrDeviceTimeout if the device doesn't accept the report in about 5 seconds.
func (d *Device) SetOutputReport(reportId byte, data []byte) error {
	if !d.isOpen() {
		return fmt.Errorf("%w [%s]", ErrDeviceIsClosed, d)
	}

	if len(data) > int(d.reportOutputLength) {
		return fmt.Errorf("%w [%s]", ErrReportBufferOverflow, d)
	}

	if err := d.setOutputReport(reportId, data); err != nil {
		return fmt.Errorf("%w [rid=%d; %s]: %w", ErrSetOutputReportFailed, reportId, d, err)
	}
	return nil
}

// GetFeatureReport reads a feature report from the USB HID device.
// It may block until a report is available, depending on the operating system.
// It takes the desired report id and returns a slice of bytes with the report
// content and an error (or nil).
func (d *Device) GetFeatureReport(reportId byte) ([]byte, error) {
	if !d.isOpen() {
		return nil, fmt.Errorf("%w [%s]", ErrDeviceIsClosed, d)
	}

	buf, err := d.getFeatureReport(reportId)
	if err != nil {
		return nil, fmt.Errorf("%w [rid=%d; %s]: %w", ErrGetFeatureReportFailed, reportId, d, err)
	}
	return buf, nil
}

// SetFeatureReport writes a feature report to the USB HID device.
// It takes the report id and a slice of bytes with the data to be sent,
// and returns an error or nil. If the slice is bigger than the expected
// report size, an error is returned. Any padding required by the operating
// system is handled internally. It fails with an error matching
// ErrDeviceTimeout if the device doesn't accept the report in about 5 seconds.
func (d *Device) SetFeatureReport(reportId byte, data []byte) error {
	if !d.isOpen() {
		return fmt.Errorf("%w [%s]", ErrDeviceIsClosed, d)
	}

	if len(data) > int(d.reportFeatureLength) {
		return fmt.Errorf("%w [%s]", ErrReportBufferOverflow, d)
	}

	if err := d.setFeatureReport(reportId, data); err != nil {
		return fmt.Errorf("%w [rid=%d; %s]: %w", ErrSetFeatureReportFailed, reportId, d, err)
	}
	return nil
}

// GetInputReportLength returns the data size of an input report in bytes.
func (d *Device) GetInputReportLength() uint16 {
	return d.reportInputLength
}

// GetOutputReportLength returns the data size of an output report in bytes.
func (d *Device) GetOutputReportLength() uint16 {
	return d.reportOutputLength
}

// GetFeatureReportLength returns the data size of a feature report in bytes.
func (d *Device) GetFeatureReportLength() uint16 {
	return d.reportFeatureLength
}

// Path returns a string representation of the USB HID device path.
func (d *Device) Path() string {
	return d.path
}

// VendorId returns the vendor identifier of the USB HID device.
func (d *Device) VendorId() uint16 {
	return d.vendorId
}

// ProductId returns the product identifier of the USB HID device.
func (d *Device) ProductId() uint16 {
	return d.productId
}

// Version returns a BCD representation of the product version of
// the USB HID device.
func (d *Device) Version() uint16 {
	return d.version
}

// Manufacturer returns a string representation of the manufacturer of
// the USB HID device.
func (d *Device) Manufacturer() string {
	return d.manufacturer
}

// Product returns a string representation of the product name of
// the USB HID device.
func (d *Device) Product() string {
	return d.product
}

// SerialNumber returns a string representation of the serial number of
// the USB HID device.
func (d *Device) SerialNumber() string {
	return d.serialNumber
}

// UsagePage returns the usage page of the USB HID device. For a device with
// several top-level collections it is the one of the first collection, so a
// later collection's usage page is not visible here.
func (d *Device) UsagePage() uint16 {
	return d.usagePage
}

// Usage returns the usage identifier of the USB HID device.
func (d *Device) Usage() uint16 {
	return d.usage
}
