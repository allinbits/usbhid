// Copyright 2022-2026 Rafael G. Martins. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !ios

package usbhid

import (
	"errors"
	"testing"
	"time"
)

// a device without input reports would otherwise block forever on some
// platforms, or return empty reports in a tight loop on others.
func TestGetInputReportWithoutInputReports(t *testing.T) {
	d := &Device{}
	fakeOpen(d)

	result := make(chan error, 1)
	go func() {
		_, _, err := d.GetInputReport()
		result <- err
	}()

	select {
	case err := <-result:
		if !errors.Is(err, ErrGetInputReportFailed) {
			t.Fatalf("got %v, want ErrGetInputReportFailed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked waiting for a report that can never arrive")
	}
}

func TestEnumerateIsSortedByPath(t *testing.T) {
	original := enumerateDevices
	defer func() { enumerateDevices = original }()
	enumerateDevices = func() ([]*Device, error) {
		return []*Device{{path: "c"}, {path: "a", vendorId: 1}, {path: "b"}}, nil
	}

	devices, err := Enumerate(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 3 || devices[0].path != "a" || devices[1].path != "b" || devices[2].path != "c" {
		t.Fatalf("not sorted: %v", devices)
	}

	devices, err = Enumerate(func(d *Device) bool { return d.path != "b" })
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 2 || devices[0].path != "a" || devices[1].path != "c" {
		t.Fatalf("filtered list not sorted: %v", devices)
	}
}
