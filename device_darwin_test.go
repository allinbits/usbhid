// Copyright 2022-2026 Rafael G. Martins. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !ios

package usbhid

import (
	"bytes"
	"errors"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// stubIOKit replaces the native functions reached by the device lifecycle with
// harmless defaults, and restores all of them when the test ends. Tests
// override only the ones whose behaviour they care about.
func stubIOKit(t *testing.T) {
	originalClose := _IOHIDDeviceClose
	originalRelease := _CFRelease
	originalRetain := _CFRetain
	originalPush := _objc_autoreleasePoolPush
	originalPop := _objc_autoreleasePoolPop
	originalSetReport := _IOHIDDeviceSetReportWithCallback
	originalGetReport := _IOHIDDeviceGetReport
	originalSignal := _CFRunLoopSourceSignal
	originalWakeUp := _CFRunLoopWakeUp
	t.Cleanup(func() {
		_IOHIDDeviceClose = originalClose
		_CFRelease = originalRelease
		_CFRetain = originalRetain
		_objc_autoreleasePoolPush = originalPush
		_objc_autoreleasePoolPop = originalPop
		_IOHIDDeviceSetReportWithCallback = originalSetReport
		_IOHIDDeviceGetReport = originalGetReport
		_CFRunLoopSourceSignal = originalSignal
		_CFRunLoopWakeUp = originalWakeUp
	})

	_IOHIDDeviceClose = func(_ _IOHIDDeviceRef, _ _IOOptionBits) _IOReturn { return kIOReturnSuccess }
	_CFRelease = func(_ _CFTypeRef) {}
	_CFRetain = func(cf _CFTypeRef) _CFTypeRef { return cf }
	_objc_autoreleasePoolPush = func() uintptr { return 1 }
	_objc_autoreleasePoolPop = func(_ uintptr) {}
	_CFRunLoopSourceSignal = func(_ _CFRunLoopSourceRef) {}
	_CFRunLoopWakeUp = func(_ _CFRunLoopRef) {}
}

func TestDarwinSystemAllocator(t *testing.T) {
	allocator := systemAllocator()
	if allocator == 0 {
		t.Fatal("kCFAllocatorSystemDefault resolved to nil")
	}
	ptr := _CFAllocatorAllocate(allocator, 16, 0)
	if ptr == nil {
		t.Fatal("CFAllocatorAllocate returned nil")
	}
	_CFAllocatorDeallocate(allocator, ptr)
}

func TestDarwinCFStringRoundTrip(t *testing.T) {
	for _, want := range []string{"ascii", "Vendor®", "日本語"} {
		b := []byte(want)
		str := _CFStringCreateWithBytes(kCFAllocatorDefault, b, _CFIndex(len(b)), kCFStringEncodingUTF8, false)
		if str == 0 {
			t.Fatalf("failed to create string for %q", want)
		}
		got, err := cfstringToString(str)
		_CFRelease(_CFTypeRef(str))
		if err != nil {
			t.Fatalf("failed to convert %q: %v", want, err)
		}
		if got != want {
			t.Fatalf("string round trip mismatch: got %q, want %q", got, want)
		}
	}
}

func TestDarwinIOReturnError(t *testing.T) {
	err := ioReturnError(-0x1ffffd44)
	if got, want := err.Error(), "(iokit/common) general error (0xe00002bc)"; got != want {
		t.Fatalf("unexpected IOReturn error: got %q, want %q", got, want)
	}
}

func TestDarwinInputCallbackCopiesNativeReport(t *testing.T) {
	d := &Device{}
	d.extra.state = deviceOpen
	d.extra.inputBufferLen = 4
	d.extra.inputCh = make(chan inputCtx, 1)
	handle := registerCallbackDevice(d)
	defer unregisterCallbackDevice(handle)

	report := []byte{3, 1, 2, 3}
	inputCallback(handle, kIOReturnSuccess, 0, 0, 3, unsafe.Pointer(&report[0]), _CFIndex(len(report)))
	report[1] = 9

	result := <-d.extra.inputCh
	if result.err != nil {
		t.Fatalf("unexpected callback error: %v", result.err)
	}
	if !bytes.Equal(result.buf, []byte{3, 1, 2, 3}) {
		t.Fatalf("callback did not copy report: %v", result.buf)
	}
}

func TestDarwinInputCallbackRejectsInvalidLength(t *testing.T) {
	d := &Device{}
	d.extra.state = deviceOpen
	d.extra.inputBufferLen = 1
	d.extra.inputCh = make(chan inputCtx, 1)
	handle := registerCallbackDevice(d)
	defer unregisterCallbackDevice(handle)

	report := []byte{1}
	inputCallback(handle, kIOReturnSuccess, 0, 0, 0, unsafe.Pointer(&report[0]), 2)
	if result := <-d.extra.inputCh; result.err == nil {
		t.Fatal("invalid callback length was accepted")
	}
}

func TestDarwinCloseUnblocksInputReader(t *testing.T) {
	stubIOKit(t)

	d := &Device{}
	d.extra.state = deviceOpen
	d.extra.file = 1
	d.extra.inputCh = make(chan inputCtx)
	d.extra.done = make(chan struct{})
	d.extra.runloopDone = make(chan struct{})
	close(d.extra.runloopDone)

	result := make(chan error, 1)
	go func() {
		_, _, err := d.getInputReport()
		result <- err
	}()

	if err := d.close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	select {
	case err := <-result:
		if !errors.Is(err, ErrDeviceIsClosed) {
			t.Fatalf("reader returned %v, want ErrDeviceIsClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("input reader remained blocked after close")
	}
}

func TestDarwinCloseWaitsForAsynchronousIOBeforeTeardown(t *testing.T) {
	stubIOKit(t)

	ioStarted := make(chan struct{})
	allowIO := make(chan struct{})
	teardownStarted := make(chan struct{}, 1)
	_IOHIDDeviceSetReportWithCallback = func(_ _IOHIDDeviceRef, typ _IOHIDReportType, reportId _CFIndex, report unsafe.Pointer, reportLength _CFIndex, _ _CFTimeInterval, _ uintptr, context uintptr) _IOReturn {
		close(ioStarted)
		<-allowIO
		reportCallback(context, kIOReturnSuccess, 0, typ, uint32(reportId), report, reportLength)
		return kIOReturnSuccess
	}
	var d *Device
	_CFRunLoopSourceSignal = func(_ _CFRunLoopSourceRef) {
		select {
		case fn := <-d.extra.ioCh:
			go fn()
		default:
			teardownStarted <- struct{}{}
			close(d.extra.runloopDone)
		}
	}
	d = &Device{}
	d.extra.state = deviceOpen
	d.extra.file = 1
	d.extra.ioSource = 1
	d.extra.ioCh = make(chan func(), 1)
	d.extra.reportCh = make(chan _IOReturn, 1)
	d.extra.done = make(chan struct{})
	d.extra.runloopDone = make(chan struct{})
	d.extra.callbackHandle = registerCallbackDevice(d)
	defer unregisterCallbackDevice(d.extra.callbackHandle)

	ioResult := make(chan error, 1)
	go func() { ioResult <- d.setFeatureReport(0, []byte{1}) }()
	<-ioStarted

	closeResult := make(chan error, 1)
	go func() { closeResult <- d.close() }()
	deadline := time.After(time.Second)
	for {
		d.extra.stateMtx.Lock()
		state := d.extra.state
		d.extra.stateMtx.Unlock()
		if state == deviceClosing {
			break
		}
		select {
		case <-deadline:
			t.Fatal("close did not enter closing state")
		default:
		}
	}

	select {
	case <-teardownStarted:
		t.Fatal("run-loop teardown started while asynchronous I/O was active")
	case <-time.After(50 * time.Millisecond):
	}

	close(allowIO)
	if err := <-ioResult; err != nil {
		t.Fatalf("asynchronous I/O failed: %v", err)
	}
	select {
	case <-teardownStarted:
	case <-time.After(time.Second):
		t.Fatal("run-loop teardown did not start after synchronous I/O completed")
	}
	if err := <-closeResult; err != nil {
		t.Fatalf("close failed: %v", err)
	}
}

func TestDarwinRemovalDuringAsynchronousIODefersRelease(t *testing.T) {
	stubIOKit(t)

	ioStarted := make(chan struct{})
	allowIO := make(chan struct{})
	released := make(chan struct{}, 1)
	_IOHIDDeviceSetReportWithCallback = func(_ _IOHIDDeviceRef, typ _IOHIDReportType, reportId _CFIndex, report unsafe.Pointer, reportLength _CFIndex, _ _CFTimeInterval, _ uintptr, context uintptr) _IOReturn {
		close(ioStarted)
		<-allowIO
		reportCallback(context, kIOReturnSuccess, 0, typ, uint32(reportId), report, reportLength)
		return kIOReturnSuccess
	}
	var d *Device
	_CFRunLoopSourceSignal = func(_ _CFRunLoopSourceRef) {
		select {
		case fn := <-d.extra.ioCh:
			go fn()
		default:
			close(d.extra.runloopDone)
		}
	}
	_CFRelease = func(_ _CFTypeRef) { released <- struct{}{} }

	d = &Device{}
	d.extra.state = deviceOpen
	d.extra.file = 1
	d.extra.ioSource = 1
	d.extra.ioCh = make(chan func(), 1)
	d.extra.reportCh = make(chan _IOReturn, 1)
	d.extra.done = make(chan struct{})
	d.extra.runloopDone = make(chan struct{})
	handle := registerCallbackDevice(d)
	d.extra.callbackHandle = handle

	ioResult := make(chan error, 1)
	go func() { ioResult <- d.setFeatureReport(0, []byte{1}) }()
	<-ioStarted

	removalCallback(handle, kIOReturnSuccess, 0)
	// A duplicate native removal notification must not close done twice.
	removalCallback(handle, kIOReturnSuccess, 0)
	select {
	case <-d.extra.done:
	case <-time.After(time.Second):
		t.Fatal("removal did not close the session channel")
	}
	select {
	case <-released:
		t.Fatal("device was released while asynchronous I/O was active")
	case <-time.After(50 * time.Millisecond):
	}

	close(allowIO)
	if err := <-ioResult; err != nil {
		t.Fatalf("asynchronous I/O failed: %v", err)
	}
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("device was not released after synchronous I/O completed")
	}
	deadline := time.After(time.Second)
	for {
		d.extra.stateMtx.Lock()
		state := d.extra.state
		d.extra.stateMtx.Unlock()
		if state == deviceClosed {
			break
		}
		select {
		case <-deadline:
			t.Fatal("automatic close did not complete")
		default:
		}
	}
}

func TestDarwinFinishOpenRejectsRemovalRace(t *testing.T) {
	d := &Device{}
	d.extra.state = deviceDisconnected

	if err := d.finishOpen(); !errors.Is(err, ErrDeviceIsClosed) {
		t.Fatalf("finishOpen returned %v, want ErrDeviceIsClosed", err)
	}
	if d.extra.state != deviceDisconnected {
		t.Fatalf("finishOpen changed disconnected state to %v", d.extra.state)
	}
}

func TestDarwinFinishOpenPublishesOpenState(t *testing.T) {
	d := &Device{}
	d.extra.state = deviceOpening

	if err := d.finishOpen(); err != nil {
		t.Fatalf("finishOpen failed: %v", err)
	}
	if d.extra.state != deviceOpen {
		t.Fatalf("finishOpen left state at %v", d.extra.state)
	}
}

func TestDarwinAsynchronousReportFraming(t *testing.T) {
	stubIOKit(t)

	var got []byte
	_IOHIDDeviceSetReportWithCallback = func(_ _IOHIDDeviceRef, typ _IOHIDReportType, reportId _CFIndex, report unsafe.Pointer, reportLength _CFIndex, _ _CFTimeInterval, _ uintptr, context uintptr) _IOReturn {
		got = append([]byte{}, unsafe.Slice((*byte)(report), int(reportLength))...)
		reportCallback(context, kIOReturnSuccess, 0, typ, uint32(reportId), report, reportLength)
		return kIOReturnSuccess
	}
	d := &Device{reportWithId: true}
	d.extra.state = deviceOpen
	d.extra.file = 1
	d.extra.runloop = 1
	d.extra.ioSource = 1
	d.extra.ioCh = make(chan func(), 1)
	d.extra.reportCh = make(chan _IOReturn, 1)
	d.extra.callbackHandle = registerCallbackDevice(d)
	defer unregisterCallbackDevice(d.extra.callbackHandle)
	go func() { (<-d.extra.ioCh)() }()
	if err := d.setFeatureReport(7, []byte{1, 2}); err != nil {
		t.Fatalf("set feature report failed: %v", err)
	}
	if !bytes.Equal(got, []byte{7, 1, 2}) {
		t.Fatalf("report framing mismatch: %v", got)
	}
}

func TestDarwinSetReportUnblocksWhenRunloopExits(t *testing.T) {
	stubIOKit(t)

	var d *Device
	_CFRunLoopSourceSignal = func(_ _CFRunLoopSourceRef) {
		// the run loop exits without ever performing the queued operation.
		close(d.extra.loopExited)
	}

	d = &Device{}
	d.extra.state = deviceOpen
	d.extra.file = 1
	d.extra.ioSource = 1
	d.extra.ioCh = make(chan func(), 1)
	d.extra.reportCh = make(chan _IOReturn, 1)
	d.extra.loopExited = make(chan struct{})
	d.extra.callbackHandle = registerCallbackDevice(d)
	defer unregisterCallbackDevice(d.extra.callbackHandle)

	result := make(chan error, 1)
	go func() { result <- d.setFeatureReport(0, []byte{1}) }()

	select {
	case err := <-result:
		if !errors.Is(err, ErrDeviceIsClosed) {
			t.Fatalf("set feature report returned %v, want ErrDeviceIsClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("set feature report remained blocked after the run loop exited")
	}
}

func TestDarwinSynchronousFeatureReportUsesReturnedLength(t *testing.T) {
	stubIOKit(t)

	_IOHIDDeviceGetReport = func(_ _IOHIDDeviceRef, _ _IOHIDReportType, _ _CFIndex, report []byte, length *_CFIndex) _IOReturn {
		copy(report, []byte{4, 8, 9})
		*length = 3
		return kIOReturnSuccess
	}

	d := &Device{reportFeatureLength: 8, reportWithId: true}
	d.extra.state = deviceOpen
	d.extra.file = 1
	report, err := d.getFeatureReport(4)
	if err != nil {
		t.Fatalf("get feature report failed: %v", err)
	}
	if !bytes.Equal(report, []byte{8, 9}) {
		t.Fatalf("feature report length mismatch: %v", report)
	}
}

func fakeOpen(d *Device) {
	d.extra.state = deviceOpen
	d.extra.done = make(chan struct{})
}

func TestDarwinFeatureReportOfMaximumLength(t *testing.T) {
	stubIOKit(t)

	_IOHIDDeviceGetReport = func(_ _IOHIDDeviceRef, _ _IOHIDReportType, _ _CFIndex, report []byte, length *_CFIndex) _IOReturn {
		if len(report) != 0x10000 {
			t.Errorf("report buffer has %d bytes", len(report))
		}
		copy(report, []byte{4, 8, 9})
		*length = 3
		return kIOReturnSuccess
	}

	d := &Device{reportFeatureLength: 0xffff, reportWithId: true}
	d.extra.state = deviceOpen
	d.extra.file = 1
	report, err := d.getFeatureReport(4)
	if err != nil {
		t.Fatalf("get feature report failed: %v", err)
	}
	if !bytes.Equal(report, []byte{8, 9}) {
		t.Fatalf("feature report mismatch: %v", report)
	}
}

func override[T any](t *testing.T, p *T, v T) {
	original := *p
	*p = v
	t.Cleanup(func() { *p = original })
}

// fakeSession stubs everything a session calls natively, so the real open,
// run-loop goroutine and close can run without a device.
type fakeSession struct {
	stop     chan struct{}
	closes   atomic.Int32
	unsched  atomic.Int32
	setCalls atomic.Int32
	timeout  atomic.Value
}

func newFakeSession(t *testing.T) *fakeSession {
	t.Helper()
	stubIOKit(t)

	f := &fakeSession{stop: make(chan struct{}, 1)}
	f.stubDevice(t)
	override(t, &_CFRunLoopSourceCreate, func(_ _CFAllocatorRef, _ _CFIndex, _ *_CFRunLoopSourceContext) _CFRunLoopSourceRef { return 1 })
	override(t, &_CFRunLoopGetCurrent, func() _CFRunLoopRef { return 1 })
	override(t, &_CFRunLoopAddSource, func(_ _CFRunLoopRef, _ _CFRunLoopSourceRef, _ _CFStringRef) {})
	override(t, &_CFRunLoopRemoveSource, func(_ _CFRunLoopRef, _ _CFRunLoopSourceRef, _ _CFStringRef) {})
	override(t, &_CFRunLoopRunInMode, func(_ _CFStringRef, _ _CFTimeInterval, _ bool) int32 {
		<-f.stop
		return kCFRunLoopRunStopped
	})
	override(t, &_CFRunLoopStop, func(_ _CFRunLoopRef) { f.stopLoop() })
	return f
}

// stubDevice stubs the calls that need a device, and nothing about run loops.
func (f *fakeSession) stubDevice(t *testing.T) {
	t.Helper()

	override(t, &_IORegistryEntryFromPath, func(_ _mach_port_t, _ _io_string_t) _io_registry_entry_t { return 1 })
	override(t, &_IOObjectRelease, func(_ _io_object_t) _kern_return_t { return 0 })
	override(t, &_IOHIDDeviceCreate, func(_ _CFAllocatorRef, _ _io_service_t) _IOHIDDeviceRef { return 1 })
	override(t, &_IOHIDDeviceOpen, func(_ _IOHIDDeviceRef, _ _IOOptionBits) _IOReturn { return kIOReturnSuccess })
	override(t, &_IOHIDDeviceScheduleWithRunLoop, func(_ _IOHIDDeviceRef, _ _CFRunLoopRef, _ _CFStringRef) {})
	override(t, &_IOHIDDeviceUnscheduleFromRunLoop, func(_ _IOHIDDeviceRef, _ _CFRunLoopRef, _ _CFStringRef) { f.unsched.Add(1) })
	override(t, &_IOHIDDeviceRegisterInputReportCallback, func(_ _IOHIDDeviceRef, _ unsafe.Pointer, _ _CFIndex, _ uintptr, _ uintptr) {})
	override(t, &_IOHIDDeviceRegisterRemovalCallback, func(_ _IOHIDDeviceRef, _ uintptr, _ uintptr) {})
	override(t, &_IOHIDDeviceClose, func(_ _IOHIDDeviceRef, _ _IOOptionBits) _IOReturn {
		f.closes.Add(1)
		return kIOReturnSuccess
	})
}

func (f *fakeSession) stopLoop() {
	select {
	case f.stop <- struct{}{}:
	default:
	}
}

// neverComplete makes writes reach iokit and never get a completion.
func (f *fakeSession) neverComplete(t *testing.T, d *Device) {
	t.Helper()

	override(t, &_CFRunLoopSourceSignal, func(_ _CFRunLoopSourceRef) {
		select {
		case fn := <-d.extra.ioCh:
			fn()
		default:
		}
	})
	override(t, &_IOHIDDeviceSetReportWithCallback, func(_ _IOHIDDeviceRef, _ _IOHIDReportType, _ _CFIndex, _ unsafe.Pointer, _ _CFIndex, timeout _CFTimeInterval, _ uintptr, _ uintptr) _IOReturn {
		f.setCalls.Add(1)
		f.timeout.Store(timeout)
		return kIOReturnSuccess
	})
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func deviceState_(d *Device) deviceState {
	d.extra.stateMtx.Lock()
	defer d.extra.stateMtx.Unlock()
	return d.extra.state
}

func openFake(t *testing.T) *Device {
	t.Helper()

	d := &Device{reportInputLength: 8, reportOutputLength: 8, reportFeatureLength: 8}
	if err := d.open(false); err != nil {
		t.Fatalf("open failed: %v", err)
	}
	return d
}

func TestDarwinUnexpectedRunloopStopReleasesWrite(t *testing.T) {
	f := newFakeSession(t)
	d := openFake(t)
	f.neverComplete(t, d)

	result := make(chan error, 1)
	go func() { result <- d.setOutputReport(0, []byte{1}) }()
	waitFor(t, "the write to reach iokit", func() bool { return f.setCalls.Load() == 1 })

	f.stopLoop()

	select {
	case err := <-result:
		if !errors.Is(err, ErrDeviceIsClosed) {
			t.Fatalf("write returned %v, want ErrDeviceIsClosed", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("write stayed blocked after the run loop stopped")
	}
	waitFor(t, "automatic close", func() bool { return deviceState_(d) == deviceClosed })
}

func TestDarwinWriteHasTimeoutAndBackstop(t *testing.T) {
	override(t, &writeTimeout, 50*time.Millisecond)
	f := newFakeSession(t)
	d := openFake(t)
	f.neverComplete(t, d)
	defer d.close()

	start := time.Now()
	result := make(chan error, 1)
	go func() { result <- d.setOutputReport(0, []byte{1}) }()

	select {
	case err := <-result:
		if !errors.Is(err, ErrDeviceTimeout) {
			t.Fatalf("write returned %v, want ErrDeviceTimeout", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("write did not time out")
	}
	if elapsed := time.Since(start); elapsed < writeTimeout {
		t.Fatalf("write gave up after %s, before iokit's own timeout", elapsed)
	}
	// IOHIDDeviceSetReportWithCallback takes milliseconds, despite the type.
	if got := f.timeout.Load().(_CFTimeInterval); got != 50 {
		t.Fatalf("iokit timeout is %v, want 50", got)
	}

	// iokit still owns that transfer: its completion must never be taken for
	// the one of a later write.
	if err := d.setOutputReport(0, []byte{1}); !errors.Is(err, ErrDeviceTimeout) {
		t.Fatalf("second write returned %v, want ErrDeviceTimeout", err)
	}
	if got := f.setCalls.Load(); got != 1 {
		t.Fatalf("%d writes reached iokit, want 1", got)
	}
}

func TestDarwinNewSessionAcceptsWritesAfterTimeout(t *testing.T) {
	override(t, &writeTimeout, 10*time.Millisecond)
	f := newFakeSession(t)
	d := openFake(t)
	f.neverComplete(t, d)

	if err := d.setOutputReport(0, []byte{1}); !errors.Is(err, ErrDeviceTimeout) {
		t.Fatalf("write returned %v, want ErrDeviceTimeout", err)
	}
	if err := d.close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}
	if err := d.open(false); err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	defer d.close()

	calls := f.setCalls.Load()
	result := make(chan error, 1)
	go func() { result <- d.setOutputReport(0, []byte{1}) }()
	waitFor(t, "the write to reach iokit", func() bool { return f.setCalls.Load() == calls+1 })
	<-result
}

func TestDarwinIOKitTimeoutIsReported(t *testing.T) {
	f := newFakeSession(t)
	d := openFake(t)
	defer d.close()

	override(t, &_CFRunLoopSourceSignal, func(_ _CFRunLoopSourceRef) {
		select {
		case fn := <-d.extra.ioCh:
			fn()
		default:
		}
	})
	override(t, &_IOHIDDeviceSetReportWithCallback, func(_ _IOHIDDeviceRef, typ _IOHIDReportType, id _CFIndex, report unsafe.Pointer, length _CFIndex, _ _CFTimeInterval, _ uintptr, context uintptr) _IOReturn {
		reportCallback(context, -0x1ffffd2a, 0, typ, uint32(id), report, length)
		return kIOReturnSuccess
	})
	_ = f

	if err := d.setOutputReport(0, []byte{1}); !errors.Is(err, ErrDeviceTimeout) {
		t.Fatalf("write returned %v, want ErrDeviceTimeout", err)
	}
}

// a thread going back to the pool would keep its run loop, with whatever was
// scheduled on it, and be picked up by a later session.
func TestDarwinRunloopThreadExitsWithSession(t *testing.T) {
	newFakeSession(t)

	// thread ids are never reused by the kernel, so a repeated one is a thread
	// that served two sessions.
	var mtx sync.Mutex
	threads := map[uintptr]bool{}
	override(t, &_CFRunLoopGetCurrent, func() _CFRunLoopRef {
		id, _, errno := syscall.RawSyscall(syscall.SYS_THREAD_SELFID, 0, 0, 0)
		if errno != 0 {
			t.Errorf("failed to get the thread id: %v", errno)
		}
		mtx.Lock()
		threads[id] = true
		mtx.Unlock()
		return 1
	})

	const sessions = 32
	for i := 0; i < sessions; i++ {
		d := openFake(t)
		if err := d.close(); err != nil {
			t.Fatalf("close failed: %v", err)
		}
	}

	mtx.Lock()
	defer mtx.Unlock()
	if len(threads) != sessions {
		t.Fatalf("%d sessions ran on %d threads", sessions, len(threads))
	}
}

func TestDarwinDisconnectedDeviceIsStillClosed(t *testing.T) {
	f := newFakeSession(t)
	d := openFake(t)

	removalCallback(d.extra.callbackHandle, kIOReturnSuccess, 0)
	waitFor(t, "automatic close", func() bool { return deviceState_(d) == deviceClosed })

	if got := f.closes.Load(); got != 1 {
		t.Fatalf("IOHIDDeviceClose called %d times, want 1", got)
	}
	if got := f.unsched.Load(); got != 1 {
		t.Fatalf("IOHIDDeviceUnscheduleFromRunLoop called %d times, want 1", got)
	}
}

func TestDarwinDisconnectedCloseErrorIsIgnored(t *testing.T) {
	newFakeSession(t)
	override(t, &_IOHIDDeviceClose, func(_ _IOHIDDeviceRef, _ _IOOptionBits) _IOReturn { return -0x1ffffd44 })
	d := openFake(t)

	d.extra.stateMtx.Lock()
	d.extra.state = deviceDisconnected
	close(d.extra.done)
	d.extra.stateMtx.Unlock()

	if err := d.close(); err != nil {
		t.Fatalf("close of a disconnected device failed: %v", err)
	}
}

func TestDarwinAutomaticCloseSparesNewerSession(t *testing.T) {
	stubIOKit(t)

	d := &Device{}
	d.extra.state = deviceOpen
	d.extra.file = 1
	d.extra.done = make(chan struct{})
	first := registerCallbackDevice(d)
	defer unregisterCallbackDevice(first)
	d.extra.callbackHandle = first

	// holding this keeps the deferred close from running until the session
	// has been replaced.
	d.extra.lifeMtx.Lock()
	removalCallback(first, kIOReturnSuccess, 0)

	second := registerCallbackDevice(d)
	defer unregisterCallbackDevice(second)
	d.extra.stateMtx.Lock()
	d.extra.state = deviceOpen
	d.extra.done = make(chan struct{})
	d.extra.callbackHandle = second
	d.extra.stateMtx.Unlock()
	d.extra.lifeMtx.Unlock()

	time.Sleep(100 * time.Millisecond)

	if got := deviceState_(d); got != deviceOpen {
		t.Fatalf("the newer session ended up in state %v", got)
	}
}

func TestDarwinRunloopRetainedUntilCloseIsDone(t *testing.T) {
	const runloop = 2

	f := newFakeSession(t)
	var retained atomic.Int32
	override(t, &_CFRunLoopGetCurrent, func() _CFRunLoopRef { return runloop })
	override(t, &_CFRetain, func(cf _CFTypeRef) _CFTypeRef {
		if cf == runloop {
			retained.Add(1)
		}
		return cf
	})
	override(t, &_CFRelease, func(cf _CFTypeRef) {
		if cf == runloop {
			retained.Add(-1)
		}
	})
	override(t, &_CFRunLoopWakeUp, func(_ _CFRunLoopRef) {
		if retained.Load() != 1 {
			t.Error("run loop woken without a reference")
		}
	})
	override(t, &_CFRunLoopStop, func(_ _CFRunLoopRef) {
		if retained.Load() != 1 {
			t.Error("run loop stopped without a reference")
		}
		f.stopLoop()
	})

	d := openFake(t)
	f.stopLoop() // an unexpected stop: close runs after the thread is gone.
	waitFor(t, "automatic close", func() bool { return deviceState_(d) == deviceClosed })

	if got := retained.Load(); got != 0 {
		t.Fatalf("%d references to the run loop left", got)
	}
}

// the same with a real run loop, whose memory goes away with its thread.
func TestDarwinCloseAfterRunloopThreadExit(t *testing.T) {
	f := &fakeSession{}
	f.stubDevice(t)
	realRelease := _CFRelease
	override(t, &_CFRelease, func(cf _CFTypeRef) {
		if cf != 1 { // the fake device
			realRelease(cf)
		}
	})

	for i := 0; i < 8; i++ {
		d := openFake(t)
		d.extra.stateMtx.Lock()
		runloop := d.extra.runloop
		d.extra.stateMtx.Unlock()

		_CFRunLoopStop(runloop)
		waitFor(t, "automatic close", func() bool { return deviceState_(d) == deviceClosed })
	}
}
