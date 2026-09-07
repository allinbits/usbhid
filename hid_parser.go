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
		rcollectionl byte
		rcount       uint32
		rsize        uint32
		rusagePage   uint32
		rusage       uint32
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
				input[rid] += rcount * rsize

			case 9: // output
				output[rid] += rcount * rsize

			case 10: // collection
				if rcollectionl == 0 {
					usageSet = true
				}
				rcollectionl++

			case 11: // feature
				feature[rid] += rcount * rsize

			case 12: // collection end
				rcollectionl--
			}

		case 1: // global
			switch tag {
			case 0: // usage page
				if rcollectionl == 0 && !usageSet {
					rusagePage = hidValue(size, descriptor[i:])
				}

			case 7: // report size
				rsize = hidValue(size, descriptor[i:])

			case 8: // report id
				rid = hidValue(size, descriptor[i:])
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
				}
			}
		}

		i += int(size)
	}

	return uint16(rusagePage), uint16(rusage), maxReportLength(input), maxReportLength(output), maxReportLength(feature), withId
}
