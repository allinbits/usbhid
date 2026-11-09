// Copyright 2022-2026 Rafael G. Martins. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package usbhid

import "errors"

// iOS matches the darwin build constraints, but it has no IOKit HID API for
// applications, and purego needs cgo there. Its own backend keeps packages
// importing this one buildable, and reports the lack of support at runtime.
var errUnsupportedPlatform = errors.New("usb hid is not supported on ios")

type deviceExtra struct{}

func enumerate() ([]*Device, error) {
	return nil, errUnsupportedPlatform
}

func (d *Device) open(lock bool) error {
	return errUnsupportedPlatform
}

func (d *Device) isOpen() bool {
	return false
}

func (d *Device) close() error {
	return errUnsupportedPlatform
}

func (d *Device) getInputReport() (byte, []byte, error) {
	return 0, nil, errUnsupportedPlatform
}

func (d *Device) setOutputReport(reportId byte, data []byte) error {
	return errUnsupportedPlatform
}

func (d *Device) getFeatureReport(reportId byte) ([]byte, error) {
	return nil, errUnsupportedPlatform
}

func (d *Device) setFeatureReport(reportId byte, data []byte) error {
	return errUnsupportedPlatform
}
