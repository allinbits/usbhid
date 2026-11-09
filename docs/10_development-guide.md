# Development guide

usbhid provides device enumeration, filtering, and bidirectional HID report communication from pure Go. This guide covers integration, the API surface, platform-specific behavior, and known limitations.

For complete function signatures and type definitions, see the [API documentation](https://pkg.go.dev/github.com/allinbits/usbhid).

## Integration

### Installation

```bash
go get github.com/allinbits/usbhid
```

The module requires Go 1.25 or later. The external dependencies are [purego](https://github.com/ebitengine/purego), used on macOS to call IOKit and CoreFoundation without cgo, and `golang.org/x/sys`, used on Windows to load system libraries.

### Import

```go
import "github.com/allinbits/usbhid"
```

## API overview

### Device enumeration

[`Enumerate`](https://pkg.go.dev/github.com/allinbits/usbhid#Enumerate) lists all USB HID devices connected to the computer. It accepts an optional [`DeviceFilterFunc`](https://pkg.go.dev/github.com/allinbits/usbhid#DeviceFilterFunc) to narrow results. Pass `nil` to return all devices. The devices are sorted by path, so the order is the same between calls for as long as the same devices are connected. It says nothing about which device was connected first.

```go
devices, err := usbhid.Enumerate(nil)
if err != nil {
    log.Fatal(err)
}

for _, device := range devices {
    fmt.Printf("Device: 0x%04x:0x%04x\n", device.VendorId(), device.ProductId())
    fmt.Printf("\tManufacturer:  %s\n", device.Manufacturer())
    fmt.Printf("\tProduct:       %s\n", device.Product())
    fmt.Printf("\tSerial Number: %s\n", device.SerialNumber())
    fmt.Printf("\tUsage:         0x%04x/0x%04x\n", device.UsagePage(), device.Usage())
}
```

### Filtering and getting a single device

[`DeviceFilterFunc`](https://pkg.go.dev/github.com/allinbits/usbhid#DeviceFilterFunc) is a function type (`func(*Device) bool`) used to select devices by their properties. It can be passed to both `Enumerate` and `Get`.

[`Get`](https://pkg.go.dev/github.com/allinbits/usbhid#Get) returns exactly one device matching the filter. It returns an error if zero or more than one device matches. It can optionally open the device and acquire an exclusive lock in the same call.

```go
device, err := usbhid.Get(func(d *usbhid.Device) bool {
    return d.VendorId() == 0x16c0 && d.ProductId() == 0x05df
}, true, true)
if err != nil {
    log.Fatal(err)
}
defer device.Close()
```

The second argument (`open`) controls whether the device is opened immediately. The third argument (`lock`) requests an exclusive lock, preventing other applications from accessing the device concurrently (only those taking the same lock on Linux, see [Linux](#linux)).

### Device properties

The [`Device`](https://pkg.go.dev/github.com/allinbits/usbhid#Device) struct is opaque. Its properties are accessed through getter methods:

| Method | Return type | Description |
|--------|-------------|-------------|
| `Path()` | `string` | Platform-specific device path |
| `VendorId()` | `uint16` | USB vendor ID |
| `ProductId()` | `uint16` | USB product ID |
| `Version()` | `uint16` | BCD-encoded product version |
| `Manufacturer()` | `string` | Manufacturer string descriptor |
| `Product()` | `string` | Product string descriptor |
| `SerialNumber()` | `string` | Serial number string descriptor |
| `UsagePage()` | `uint16` | HID usage page |
| `Usage()` | `uint16` | HID usage ID |
| `GetInputReportLength()` | `uint16` | Input report data size in bytes |
| `GetOutputReportLength()` | `uint16` | Output report data size in bytes |
| `GetFeatureReportLength()` | `uint16` | Feature report data size in bytes |

The report lengths are the size of the largest report of each type, without the report ID byte. Lengths from a descriptor declaring more than 65535 bytes saturate at 65535. On Linux and macOS, reports without IDs in several top-level collections count as one report, so their lengths add up.

On Linux and macOS, `UsagePage()` and `Usage()` describe the first top-level collection, so a device with several top-level collections on one interface doesn't show the usage pages of the others. Windows lists each top-level collection as a device of its own.

The `String()` method returns a human-readable representation including vendor ID, product ID, and available string descriptors.

### Opening and closing

[`Open`](https://pkg.go.dev/github.com/allinbits/usbhid#Device.Open) opens the device for I/O. Pass `true` to request an exclusive lock. [`Close`](https://pkg.go.dev/github.com/allinbits/usbhid#Device.Close) releases the device and any held lock. [`IsOpen`](https://pkg.go.dev/github.com/allinbits/usbhid#Device.IsOpen) reports whether the device is currently open.

```go
if err := device.Open(true); err != nil {
    log.Fatal(err)
}
defer device.Close()
```

Calling `Open` on an already-open device returns `ErrDeviceIsOpen`. Calling `Close` on a closed device returns `ErrDeviceIsClosed`. If another application holds the lock, `Open` returns `ErrDeviceLocked`.

### Concurrency

`Close` may be called while other operations are in flight. A pending `GetInputReport` returns an error matching `ErrDeviceIsClosed`. A write or feature report in flight may instead finish with its own result. Operations started after `Close` fail with `ErrDeviceIsClosed`. Reads and writes may run in different goroutines. Calls of the same kind, such as two concurrent `GetInputReport` calls, must not overlap, and `Open` must not overlap any other call on the same `Device`.

### Reading input reports

[`GetInputReport`](https://pkg.go.dev/github.com/allinbits/usbhid#Device.GetInputReport) blocks until an input report is available, then returns the report ID and the report data as a byte slice. The report ID is `0` for devices that do not use report IDs. The slice holds the bytes delivered by the operating system, so its length may differ from `GetInputReportLength()`.

```go
reportId, data, err := device.GetInputReport()
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Report ID: %d, Data: %x\n", reportId, data)
```

> [!NOTE]
> This call blocks the calling goroutine and has no timeout. To read reports without blocking other work, call it from a dedicated goroutine and communicate results through channels. To cancel it, call `Close` from another goroutine. On a device with no input reports it fails at once with an error matching `ErrGetInputReportFailed`.

Reports that arrive between calls are queued by the operating system, and are lost when the queue overflows: new reports are dropped on Linux (63 reports) and macOS (64 reports), the oldest ones on Windows (32 by default).

### Writing output reports

[`SetOutputReport`](https://pkg.go.dev/github.com/allinbits/usbhid#Device.SetOutputReport) sends an output report. Provide the report ID and the data to send. If the data is longer than `GetOutputReportLength()`, `ErrReportBufferOverflow` is returned. Any padding required by the operating system is handled internally. If the device doesn't accept the report in about 5 seconds, the error matches `ErrDeviceTimeout`.

```go
err := device.SetOutputReport(0x01, []byte{0x00, 0x01, 0x02})
if err != nil {
    log.Fatal(err)
}
```

### Feature reports

[`GetFeatureReport`](https://pkg.go.dev/github.com/allinbits/usbhid#Device.GetFeatureReport) reads a feature report by ID. [`SetFeatureReport`](https://pkg.go.dev/github.com/allinbits/usbhid#Device.SetFeatureReport) writes a feature report. The same overflow, padding and timeout rules apply to `SetFeatureReport` as to `SetOutputReport`, against `GetFeatureReportLength()`.

```go
data, err := device.GetFeatureReport(0x01)
if err != nil {
    log.Fatal(err)
}

err = device.SetFeatureReport(0x01, []byte{0x00, 0x01})
if err != nil {
    log.Fatal(err)
}
```

### Error handling

All errors returned by the library can be tested against sentinel error values using `errors.Is`:

| Error | Condition |
|-------|-----------|
| `ErrDeviceEnumerationFailed` | Device enumeration failed at the OS level |
| `ErrDeviceFailedToOpen` | Device could not be opened |
| `ErrDeviceFailedToClose` | Device could not be closed |
| `ErrDeviceIsOpen` | `Open` called on an already-open device |
| `ErrDeviceIsClosed` | I/O or `Close` called on a closed device |
| `ErrDeviceLocked` | Device is locked by another application |
| `ErrDeviceTimeout` | The device did not accept a report in time |
| `ErrNoDeviceFound` | `Get` found zero matching devices |
| `ErrMoreThanOneDeviceFound` | `Get` found more than one matching device |
| `ErrReportBufferOverflow` | Report data exceeds the expected size |
| `ErrGetInputReportFailed` | Input report read failed |
| `ErrGetFeatureReportFailed` | Feature report read failed |
| `ErrSetOutputReportFailed` | Output report write failed |
| `ErrSetFeatureReportFailed` | Feature report write failed |

Errors wrap the sentinel values, so `errors.Is(err, usbhid.ErrDeviceLocked)` works even when the error includes additional context.

## Platform details

### Linux

The Linux backend enumerates devices by walking `/sys/bus/usb/devices` and reading sysfs attributes (`idVendor`, `idProduct`, `bcdDevice`, `manufacturer`, `product`, `serial`). HID report descriptors are read from sysfs and parsed to extract usage page, usage, and report sizes. Devices are accessed through `/dev/hidrawN` nodes using standard file I/O and ioctl.

Exclusive locking uses `flock(2)` with `LOCK_EX|LOCK_NB`. It is advisory: applications that don't take the same lock, such as those using hidapi, can still open the device, and hidraw delivers every input report to every open descriptor. Without the lock option, `Open` takes a shared lock, which fails while another application holds the exclusive one.

The kernel reuses hidraw minors, so `Open` compares the vendor and product behind the node, through `HIDIOCGRAWINFO`, with the enumerated ones, and fails if they differ. The file stays in non-blocking mode and registered with the Go runtime poller, which is what lets `Close` interrupt a read. Write timeouts are the kernel's own, reported as `ErrDeviceTimeout`.

Feature reports go through an ioctl whose size field limits them to 16382 bytes of data, or 8190 on architectures with a 13-bit size field (`mips`, `ppc` and `sparc` families). Longer ones are rejected with an error, even if the device declares them.

The ioctl encoding is architecture-aware and supports: `386`, `amd64`, `arm`, `arm64`, `loong64`, `riscv64`, `s390x`, `mips`, `mips64`, `mips64le`, `mipsle`, `ppc`, `ppc64`, `ppc64le`, and `sparc64`. On any other architecture, the package panics during initialization.

> [!NOTE]
> Many USB HID devices are inaccessible to regular users by default on Linux. Depending on the device, the user may need to be added to a specific group (e.g. `plugdev` or `input`) or create a udev rule to grant access. This is outside the scope of this library.

### macOS

The macOS backend uses IOKit's HID Manager through [purego](https://github.com/ebitengine/purego) to call CoreFoundation and IOKit functions without cgo. Device properties are read from the IOKit registry. HID report descriptors are obtained from the `ReportDescriptor` property and parsed by the library.

A single `IOHIDManager` is created for the process when the package is initialized. Each `Enumerate` call resets its device matching to rebuild the device set, so devices plugged in or removed between calls are picked up. Only devices whose `Transport` property is `USB` are listed.

Each open device has a dedicated goroutine, locked to its OS thread, that runs a CFRunLoop. The thread ends with the session, so a later session never runs on a run loop that served a previous one. Input report and device removal callbacks are delivered on it, and output and feature report writes are dispatched to it, with a 5 second timeout given to IOKit. A write whose completion doesn't come within a second of that returns `ErrDeviceTimeout`, and the session then refuses further writes with the same error, because IOKit still owns that transfer. Its report buffer stays allocated for the life of the process, as IOKit may still write to it. `Close` waits for a write in flight, so it takes up to about 6 seconds in that case. `GetFeatureReport` is synchronous and runs on the calling thread instead, serialized with the writes, as running it on the run loop would stall input delivery. Input reports that arrive between `GetInputReport` calls are queued, up to 64 reports; when the queue is full, new reports are dropped.

When the device is unplugged, the library closes it automatically, and a blocking `GetInputReport` call returns `ErrDeviceIsClosed`. If the run loop stops for any other reason, the session ends in the same way. An automatic close only acts on the session it belongs to, never on a newer one opened on the same `Device`.

Exclusive locking uses `kIOHIDOptionsTypeSeizeDevice`.

iOS has no such API. The package builds there, and `Enumerate` returns an error.

### Windows

The Windows backend uses SetupAPI for device enumeration and `hid.dll` for HID-specific operations. The device list is built from the present device interfaces of the HID class (`HidD_GetHidGuid`), without filtering by transport. Device attributes and capabilities are obtained through `HidD_GetAttributes` and `HidP_GetCaps`. I/O uses overlapped operations through the kernel32 API. All libraries are loaded from the system directory only, so a DLL placed next to the executable is never used.

Writes and feature reports are cancelled after 5 seconds, returning `ErrDeviceTimeout`. `Close` cancels whatever is in flight and waits for it to end before closing any handle. In both cases the buffers have to outlive the operation, so a driver that doesn't honor the cancellation keeps the call waiting for as long as it takes. A failure listing the device interfaces, other than reaching the end of the list, is an error rather than a shorter list. A device whose own details can't be read, for example because it has just been removed, is left out of the list.

Windows expects output and feature report buffers of the full report length, so shorter data is zero-padded internally.

Exclusive locking opens the device handle with a share mode of zero, so the kernel denies every other process access to the device until the handle is closed.

> [!WARNING]
> Windows does not allow user-space applications to access full HID report descriptors. As a result, per-report sizes are unknown to the library on this platform -- only the largest possible size for each report category (input, output, feature) is available. Library consumers should know the report descriptors for their target devices and ensure data adheres to them, or implement their own validation.

## Source files

| File | Purpose |
|------|---------|
| `device.go` | Platform-independent API: `Device` struct, `Enumerate`, `Get`, report methods, error definitions |
| `device_linux.go` | Linux backend: sysfs enumeration, hidraw I/O, ioctl |
| `device_darwin.go` | macOS backend: IOKit HID Manager, CoreFoundation bindings via purego, CFRunLoop-based input |
| `device_windows.go` | Windows backend: SetupAPI enumeration, HID.dll operations, overlapped I/O |
| `device_ios.go` | iOS backend: reports that the platform is not supported |
| `hid_parser.go` | HID report descriptor parser: extracts usage page, usage, and report sizes |
| `hid_parser_test.go` | Tests for the HID report descriptor parser |
| `device_test.go` | Tests for the platform-independent API |
| `device_darwin_test.go` | Tests for the macOS backend: device lifecycle, callbacks, report framing |
| `device_linux_test.go` | Tests for the Linux backend, with a FIFO in place of a hidraw node |
| `device_windows_test.go` | Tests for the Windows backend, with a named pipe in place of a HID handle |
| `platform_test.go` | Build and dependency checks |
| `example_test.go` | Examples for `Enumerate` and `DeviceFilterFunc` |

## Known limitations

- **Blocking API.** All I/O operations block the calling goroutine. To interact with devices asynchronously, use goroutines, channels, or synchronization primitives. On every platform, `Close` unblocks a pending `GetInputReport`, which returns `ErrDeviceIsClosed`.
- **Report size validation.** `SetOutputReport` and `SetFeatureReport` only reject data longer than the largest report of that type; sizes are not validated per report ID on any platform. On Windows this information is not available at all, since the full report descriptor is not exposed to user space.
- **Linux device permissions.** Some HID devices require additional permissions. The library does not manage udev rules or group membership.
