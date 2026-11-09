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
