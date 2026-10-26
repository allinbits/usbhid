# usbhid

A pure Go library for interacting with USB HID devices, supporting Linux, macOS, and Windows.

For documentation please visit: https://rafaelmartins.com/p/usbhid/

## About this fork

This is the All in Bits fork of [rafaelmartins.com/p/usbhid](https://rafaelmartins.com/p/usbhid/)
(upstream: https://git.rafaelmartins.com/rafaelmartins/usbhid.git), forked at
commit 9e34b6c698d7d03a9b6e28d271409e8a8381bf60. Its only change is the module path,
`github.com/allinbits/usbhid`. The atomone-sdk uses it to talk to Ledger devices,
so changes and updates from upstream are reviewed as security-relevant.
