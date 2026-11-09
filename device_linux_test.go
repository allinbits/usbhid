// Copyright 2022-2026 Rafael G. Martins. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package usbhid

import (
	"bytes"
	"os"
	"testing"
)

func fakeOpen(d *Device) {
	d.extra.file = os.Stdin
}

func TestLinuxInputReportOfMaximumLength(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()

	if _, err := w.Write([]byte{4, 8, 9}); err != nil {
		t.Fatal(err)
	}

	d := &Device{reportInputLength: 0xffff, reportWithId: true}
	d.extra.file = r
	id, report, err := d.getInputReport()
	if err != nil {
		t.Fatalf("get input report failed: %v", err)
	}
	if id != 4 || !bytes.Equal(report, []byte{8, 9}) {
		t.Fatalf("input report mismatch: id=%d report=%v", id, report)
	}
}
