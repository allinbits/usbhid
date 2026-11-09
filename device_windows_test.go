// Copyright 2022-2026 Rafael G. Martins. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package usbhid

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func fakeOpen(d *Device) {
	d.extra.file = 1
}

var pipeCounter atomic.Uint32

// openPipeDevice opens the server end of a named pipe as a device. A pipe
// behaves as a HID handle does for what is tested here: reads stay pending
// until the other end writes, and with no buffer so do writes.
func openPipeDevice(t *testing.T, bufferSize uint32) (*Device, windows.Handle) {
	t.Helper()

	name := fmt.Sprintf(`\\.\pipe\usbhid-test-%d-%d`, os.Getpid(), pipeCounter.Add(1))
	peer := createPipeInstance(t, name, bufferSize)

	d := &Device{path: name, reportInputLength: 8, reportOutputLength: 8, reportFeatureLength: 8, reportWithId: true}
	if err := d.Open(false); err != nil {
		t.Fatalf("failed to open device: %v", err)
	}
	return d, peer
}

func createPipeInstance(t *testing.T, name string, bufferSize uint32) windows.Handle {
	t.Helper()

	nameW, err := windows.UTF16PtrFromString(name)
	if err != nil {
		t.Fatal(err)
	}
	peer, err := windows.CreateNamedPipe(nameW, windows.PIPE_ACCESS_DUPLEX, windows.PIPE_TYPE_BYTE|windows.PIPE_WAIT, windows.PIPE_UNLIMITED_INSTANCES, bufferSize, bufferSize, 0, nil)
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	t.Cleanup(func() { windows.CloseHandle(peer) })
	return peer
}

func waitResult(t *testing.T, result <-chan error, limit time.Duration, what string) error {
	t.Helper()

	select {
	case err := <-result:
		return err
	case <-time.After(limit):
		t.Fatalf("%s did not return within %s", what, limit)
		return nil
	}
}

func TestWindowsReportFraming(t *testing.T) {
	d, peer := openPipeDevice(t, 4096)
	defer d.Close()

	if err := d.SetOutputReport(7, []byte{1, 2}); err != nil {
		t.Fatalf("set output report failed: %v", err)
	}
	got := make([]byte, 16)
	var n uint32
	if err := windows.ReadFile(peer, got, &n, nil); err != nil {
		t.Fatal(err)
	}
	if want := []byte{7, 1, 2, 0, 0, 0, 0, 0, 0}; !bytes.Equal(got[:n], want) {
		t.Fatalf("output report mismatch: got %v, want %v", got[:n], want)
	}

	if err := windows.WriteFile(peer, []byte{4, 8, 9}, &n, nil); err != nil {
		t.Fatal(err)
	}
	id, report, err := d.GetInputReport()
	if err != nil {
		t.Fatalf("get input report failed: %v", err)
	}
	if id != 4 || !bytes.Equal(report, []byte{8, 9}) {
		t.Fatalf("input report mismatch: id=%d report=%v", id, report)
	}
}

func TestWindowsCloseUnblocksPendingRead(t *testing.T) {
	d, _ := openPipeDevice(t, 4096)

	result := make(chan error, 1)
	go func() {
		_, _, err := d.GetInputReport()
		result <- err
	}()
	time.Sleep(100 * time.Millisecond)

	closed := make(chan error, 1)
	go func() { closed <- d.Close() }()

	if err := waitResult(t, result, 3*time.Second, "pending read"); !errors.Is(err, ErrDeviceIsClosed) {
		t.Fatalf("read returned %v, want ErrDeviceIsClosed", err)
	}
	if err := waitResult(t, closed, 3*time.Second, "close"); err != nil {
		t.Fatalf("close failed: %v", err)
	}
}

func TestWindowsWriteTimesOut(t *testing.T) {
	original := writeTimeout
	writeTimeout = 200 * time.Millisecond
	defer func() { writeTimeout = original }()

	d, _ := openPipeDevice(t, 0)
	defer d.Close()

	result := make(chan error, 1)
	go func() { result <- d.SetOutputReport(0, make([]byte, 8)) }()

	err := waitResult(t, result, 3*time.Second, "write to a device that never reads")
	if !errors.Is(err, ErrDeviceTimeout) || !errors.Is(err, ErrSetOutputReportFailed) {
		t.Fatalf("write returned %v, want ErrDeviceTimeout", err)
	}
}

func TestWindowsConcurrentCloseIsSafe(t *testing.T) {
	d, _ := openPipeDevice(t, 4096)

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.GetFeatureReportLength()
			d.IsOpen()
		}()
	}

	reader := make(chan error, 1)
	go func() {
		_, _, err := d.GetInputReport()
		reader <- err
	}()
	time.Sleep(50 * time.Millisecond)

	closes := make(chan error, 8)
	for i := 0; i < cap(closes); i++ {
		go func() { closes <- d.Close() }()
	}

	succeeded := 0
	for i := 0; i < cap(closes); i++ {
		err := waitResult(t, closes, 3*time.Second, "close")
		if err == nil {
			succeeded++
		} else if !errors.Is(err, ErrDeviceIsClosed) {
			t.Fatalf("close returned %v", err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("%d closes succeeded, want 1", succeeded)
	}
	waitResult(t, reader, 3*time.Second, "pending read")
	wg.Wait()
	if d.IsOpen() {
		t.Fatal("device still open")
	}
}

func TestWindowsCloseReleasesEveryHandleOnError(t *testing.T) {
	d, _ := openPipeDevice(t, 4096)

	real := d.extra.file
	defer windows.CloseHandle(windows.Handle(real))
	d.extra.file = 0x7ffffff0 // not a handle: closing it fails

	if err := d.Close(); !errors.Is(err, ErrDeviceFailedToClose) {
		t.Fatalf("close returned %v, want ErrDeviceFailedToClose", err)
	}
	if d.IsOpen() {
		t.Fatal("device still open after a failed close")
	}
	for i, h := range []uintptr{d.extra.file, d.extra.evInput, d.extra.evOutput, d.extra.evGetFeature, d.extra.evSetFeature} {
		if h != null {
			t.Errorf("handle %d not released", i)
		}
	}
}

// CreateEventW on an existing name succeeds and sets the last error.
func TestWindowsCallJudgesSuccessByReturnValue(t *testing.T) {
	name, err := windows.UTF16FromString(fmt.Sprintf("usbhid-test-event-%d", os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}

	first, err := call(_CreateEventW, null, 1, 0, unsafe.Pointer(&name[0]))
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}
	defer windows.CloseHandle(windows.Handle(first))

	second, err := call(_CreateEventW, null, 1, 0, unsafe.Pointer(&name[0]))
	if err != nil {
		t.Fatalf("create of an existing event reported as failed: %v", err)
	}
	if second == null {
		t.Fatal("no handle returned")
	}
	windows.CloseHandle(windows.Handle(second))
}

func TestWindowsEnumerateFinishes(t *testing.T) {
	if _, err := Enumerate(nil); err != nil {
		t.Fatalf("enumerate failed: %v", err)
	}
}

func TestWindowsFailureConventions(t *testing.T) {
	for _, tt := range []struct {
		name   string
		f      failure
		r      uintptr
		failed bool
	}{
		{"BOOL false", failsBOOL, 0, true},
		{"BOOL true", failsBOOL, 1, false},
		{"BOOL upper bits are undefined", failsBOOL, uintptr(uint64(0xffffffff00000000) & uint64(^uintptr(0))), true},
		{"BOOLEAN false", failsBOOLEAN, 0, true},
		{"BOOLEAN true", failsBOOLEAN, 1, false},
		{"BOOLEAN upper bits are undefined", failsBOOLEAN, 0xffffff00, true},
		{"invalid handle", failsHandle, ^uintptr(0), true},
		{"null is a value for handle functions", failsHandle, 0, false},
		{"null", failsNull, 0, true},
		{"not null", failsNull, ^uintptr(0), false},
		{"wait failed", failsWait, uintptr(_WAIT_FAILED), true},
		{"wait timed out", failsWait, uintptr(_WAIT_TIMEOUT), false},
		{"never", failsNever, 0, false},
	} {
		if got := tt.f.failed(tt.r); got != tt.failed {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.failed)
		}
	}
}

func TestWindowsDetailSizeValidation(t *testing.T) {
	for size, want := range map[uintptr]bool{0: false, 2: false, 6: false, 7: false, 8: true, 128: true} {
		if got := validDetailSize(size); got != want {
			t.Errorf("size %d: got %v, want %v", size, got, want)
		}
	}
}

func TestWindowsShortWriteIsAnError(t *testing.T) {
	if err := checkWritten(9, 9); err != nil {
		t.Fatalf("complete write rejected: %v", err)
	}
	if err := checkWritten(8, 9); err == nil {
		t.Fatal("short write accepted")
	}
}

func TestWindowsWriteAfterCloseFails(t *testing.T) {
	d, _ := openPipeDevice(t, 4096)
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if err := d.SetOutputReport(0, []byte{1}); !errors.Is(err, ErrDeviceIsClosed) {
		t.Fatalf("got %v, want ErrDeviceIsClosed", err)
	}
}

func TestWindowsReopenAfterClose(t *testing.T) {
	d, _ := openPipeDevice(t, 4096)
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	createPipeInstance(t, d.path, 4096)
	if err := d.Open(false); err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
}
