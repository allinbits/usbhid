// Copyright 2022-2024 Rafael G. Martins. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package usbhid

func hidValue(size byte, buf []byte) uint32 {
	switch size {
	case 0:
		return 0
	case 1:
		return uint32(buf[0])
	case 2:
		return uint32(buf[1])<<8 | uint32(buf[0])
	case 4:
		return uint32(buf[3])<<24 | uint32(buf[2])<<16 | uint32(buf[1])<<8 | uint32(buf[0])
	}
	return 0
}

type hidGlobalState struct {
	size  uint32
	id    uint32
	count uint32
}

// maxReportBits is the largest report size that can be represented, as report
// lengths are 16 bits wide.
const maxReportBits = 0xffff * 8

// addReportBits accumulates the size of a main item into its report. Descriptors
// come from the device and may declare any size: the multiplication can't
// overflow 64 bits, and the result saturates.
func addReportBits(m map[uint32]uint32, id uint32, count uint32, size uint32) {
	bits := uint64(m[id]) + uint64(count)*uint64(size)
	if bits > maxReportBits {
		bits = maxReportBits
	}
	m[id] = uint32(bits)
}

func maxReportLength(m map[uint32]uint32) uint16 {
	var rv uint16
	for _, bits := range m {
		if res := uint16((bits + 7) / 8); res > rv {
			rv = res
		}
	}
	return rv
}

func hidParseReportDescriptor(descriptor []byte) (uint16, uint16, uint16, uint16, uint16, bool) {
	var (
		withId       bool
		usageSet     bool
		rcollectionl int
		rcount       uint32
		rsize        uint32
		rusagePage   uint32
		rusage       uint32
		rextended    bool
		rid          uint32
		rstack       []hidGlobalState

		input   = map[uint32]uint32{}
		output  = map[uint32]uint32{}
		feature = map[uint32]uint32{}
	)

	for i := 0; i < len(descriptor); {
		var (
			tag  = (descriptor[i] & 0b11110000) >> 4
			typ  = (descriptor[i] & 0b1100) >> 2
			size = descriptor[i] & 0b11
		)

		i++

		if size == 3 {
			size = 4
		}

		// long items carry no tag defined by the specification. skip the item
		// tag and the payload, whose size is stored right after the prefix.
		if tag == 15 && typ == 3 {
			if i >= len(descriptor) {
				break
			}
			i += 2 + int(descriptor[i])
			continue
		}

		if i+int(size) > len(descriptor) {
			break
		}

		switch typ {
		case 0: // main
			switch tag {
			case 8: // input
				addReportBits(input, rid, rcount, rsize)

			case 9: // output
				addReportBits(output, rid, rcount, rsize)

			case 10: // collection
				if rcollectionl == 0 {
					usageSet = true
				}
				rcollectionl++

			case 11: // feature
				addReportBits(feature, rid, rcount, rsize)

			case 12: // collection end
				if rcollectionl > 0 {
					rcollectionl--
				}
			}

		case 1: // global
			switch tag {
			case 0: // usage page
				if v := hidValue(size, descriptor[i:]); rcollectionl == 0 && !usageSet && v <= 0xffff {
					rusagePage = v
				}

			case 7: // report size
				rsize = hidValue(size, descriptor[i:])

			case 8: // report id
				rid = hidValue(size, descriptor[i:]) & 0xff
				withId = true

			case 9: // report count
				rcount = hidValue(size, descriptor[i:])

			case 10: // push
				rstack = append(rstack, hidGlobalState{rsize, rid, rcount})

			case 11: // pop
				if l := len(rstack); l > 0 {
					g := rstack[l-1]
					rstack = rstack[:l-1]
					rsize, rid, rcount = g.size, g.id, g.count
				}
			}

		case 2: // local
			switch tag {
			case 0: // usage
				if rcollectionl == 0 && !usageSet {
					rusage = hidValue(size, descriptor[i:])
					// a 4 byte usage carries its own page in the upper half.
					rextended = size == 4
				}
			}
		}

		i += int(size)
	}

	if rextended {
		rusagePage = rusage >> 16
	}

	return uint16(rusagePage), uint16(rusage), maxReportLength(input), maxReportLength(output), maxReportLength(feature), withId
}
