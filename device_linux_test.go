// Copyright 2022-2026 Rafael G. Martins. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package usbhid

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
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

// newFifoDevice returns a closed device backed by a FIFO, which can be polled
// and locked as a hidraw node can, and whose reads block until there is data.
func newFifoDevice(t *testing.T) *Device {
	t.Helper()

	path := filepath.Join(t.TempDir(), "hidraw0")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}

	original := hidrawGetInfo
	hidrawGetInfo = func(*os.File) (hidrawDevinfo, error) {
		return hidrawDevinfo{vendor: int16(-0x7000), product: 1}, nil
	}
	t.Cleanup(func() { hidrawGetInfo = original })

	return &Device{path: path, vendorId: 0x9000, productId: 1, reportInputLength: 8, reportOutputLength: 8, reportFeatureLength: 8}
}

func TestLinuxCloseUnblocksPendingRead(t *testing.T) {
	d := newFifoDevice(t)
	if err := d.Open(true); err != nil {
		t.Fatalf("open failed: %v", err)
	}

	result := make(chan error, 1)
	go func() {
		_, _, err := d.GetInputReport()
		result <- err
	}()
	time.Sleep(100 * time.Millisecond)

	if err := d.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	select {
	case err := <-result:
		if !errors.Is(err, ErrDeviceIsClosed) {
			t.Fatalf("read returned %v, want ErrDeviceIsClosed", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("pending read did not return after close")
	}

	// the lock must be gone with the descriptor.
	if err := d.Open(true); err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}
}

func TestLinuxExclusiveLock(t *testing.T) {
	d := newFifoDevice(t)
	if err := d.Open(true); err != nil {
		t.Fatalf("open failed: %v", err)
	}
	defer d.Close()

	other := &Device{path: d.path, vendorId: d.vendorId, productId: d.productId}
	if err := other.Open(true); !errors.Is(err, ErrDeviceLocked) {
		t.Fatalf("second open returned %v, want ErrDeviceLocked", err)
	}
}

func TestLinuxOpenRejectsAnotherDevice(t *testing.T) {
	d := newFifoDevice(t)
	d.productId = 2

	if err := d.Open(true); !errors.Is(err, ErrDeviceFailedToOpen) {
		t.Fatalf("open returned %v, want ErrDeviceFailedToOpen", err)
	}
	if d.IsOpen() {
		t.Fatal("device left open")
	}

	// the descriptor, and so the lock, must have been released.
	d.productId = 1
	if err := d.Open(true); err != nil {
		t.Fatalf("open of the right device failed: %v", err)
	}
	d.Close()
}

func TestLinuxFailedCloseLeavesDeviceClosed(t *testing.T) {
	d := newFifoDevice(t)
	if err := d.Open(false); err != nil {
		t.Fatalf("open failed: %v", err)
	}

	f := d.extra.file
	f.Close() // makes the close below fail

	if err := d.Close(); !errors.Is(err, ErrDeviceFailedToClose) {
		t.Fatalf("close returned %v, want ErrDeviceFailedToClose", err)
	}
	if d.IsOpen() {
		t.Fatal("device still open after a failed close")
	}
}

func TestLinuxOversizedFeatureReportIsRejected(t *testing.T) {
	d := newFifoDevice(t)
	d.reportFeatureLength = 0xffff
	if err := d.Open(false); err != nil {
		t.Fatalf("open failed: %v", err)
	}
	defer d.Close()

	err := d.SetFeatureReport(0, make([]byte, 0xffff))
	if err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("got %v, want a length error", err)
	}
	if _, err := d.GetFeatureReport(0); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("got %v, want a length error", err)
	}
}

func TestLinuxIoctlEncoding(t *testing.T) {
	if iocSizeBits != 14 {
		t.Skip("encoding differs on this architecture")
	}
	for _, tt := range []struct {
		got  uint32
		want uint32
	}{
		{ioc(iocRead, 'H', 0x03, 8), 0x80084803},
		{ioc(iocWrite|iocRead, 'H', 0x06, 65), 0xc0414806},
		{ioc(iocWrite|iocRead, 'H', 0x07, 65), 0xc0414807},
	} {
		if tt.got != tt.want {
			t.Errorf("got 0x%08x, want 0x%08x", tt.got, tt.want)
		}
	}
	if s := unsafe.Sizeof(hidrawDevinfo{}); s != 8 {
		t.Errorf("hidraw_devinfo has %d bytes", s)
	}
}

func TestLinuxIOErrorTranslation(t *testing.T) {
	d := &Device{}
	d.extra.file = os.Stdin

	if err := d.ioError(fmt.Errorf("write: %w", syscall.ETIMEDOUT)); !errors.Is(err, ErrDeviceTimeout) {
		t.Errorf("timeout: got %v", err)
	}
	if err := d.ioError(fmt.Errorf("read: %w", os.ErrClosed)); !errors.Is(err, ErrDeviceIsClosed) {
		t.Errorf("closed: got %v", err)
	}
	other := errors.New("other")
	if err := d.ioError(other); err != other {
		t.Errorf("other: got %v", err)
	}

	// RawConn.Control reports a closed file with an error matching nothing.
	d.extra.file = nil
	if err := d.ioError(other); !errors.Is(err, ErrDeviceIsClosed) {
		t.Errorf("closed device: got %v", err)
	}
}

func TestLinuxFeatureReportAfterCloseFails(t *testing.T) {
	d := newFifoDevice(t)
	if err := d.Open(false); err != nil {
		t.Fatalf("open failed: %v", err)
	}
	f := d.extra.file
	f.Close()
	d.extra.file = nil

	if _, err := d.getFeatureReport(0); !errors.Is(err, ErrDeviceIsClosed) {
		t.Fatalf("got %v, want ErrDeviceIsClosed", err)
	}
	if err := d.setFeatureReport(0, []byte{1}); !errors.Is(err, ErrDeviceIsClosed) {
		t.Fatalf("got %v, want ErrDeviceIsClosed", err)
	}
}
