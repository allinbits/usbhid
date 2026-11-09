# usbhid

A pure Go library for interacting with USB HID devices, supporting Linux, macOS, and Windows.

For documentation see [docs](docs/00_index.md).

## About this fork

This is the All in Bits fork of [rafaelmartins.com/p/usbhid](https://rafaelmartins.com/p/usbhid/)
(upstream: https://git.rafaelmartins.com/rafaelmartins/usbhid.git), forked at
commit 9e34b6c698d7d03a9b6e28d271409e8a8381bf60, with its own module path,
`github.com/allinbits/usbhid`, and the fixes from the
[audit](https://github.com/allinbits/usbhid/issues/1). The atomone-sdk uses it to
talk to Ledger devices, so changes and updates from upstream are reviewed as
security-relevant.

### Maintenance

Without cgo on macOS, [purego](https://github.com/ebitengine/purego) replaces
part of the Go runtime's thread start-up and links into its internals. Check it
against every Go upgrade, and run the tests with `CGO_ENABLED=0` on macOS when
updating either.

The real device paths need hardware: the tests use named pipes on Windows and
FIFOs on Linux in place of a HID device, and stubbed IOKit calls on macOS.
