// Copyright 2022-2024 Rafael G. Martins. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package usbhid

import "testing"

var (
	b8 = []byte{
		0x05, 0x0C, // UsagePage(Consumer[0x000C])
		0x09, 0x01, // UsageId(Consumer Control[0x0001])
		0xA1, 0x01, // Collection(Application)
		0x85, 0x01, //     ReportId(1)
		0x09, 0x03, //     UsageId(Programmable Buttons[0x0003])
		0xA1, 0x02, //     Collection(Logical)
		0x05, 0x09, //         UsagePage(Button[0x0009])
		0x19, 0x01, //         UsageIdMin(Button 1[0x0001])
		0x29, 0x08, //         UsageIdMax(Button 8[0x0008])
		0x15, 0x00, //         LogicalMinimum(0)
		0x25, 0x01, //         LogicalMaximum(1)
		0x95, 0x08, //         ReportCount(8)
		0x75, 0x01, //         ReportSize(1)
		0x81, 0x02, //         Input(Data, Variable, Absolute, NoWrap, Linear, PreferredState, NoNullPosition, BitField)
		0xC0,       //     EndCollection()
		0x05, 0x08, //     UsagePage(LED[0x0008])
		0x09, 0x3C, //     UsageId(Usage Multi Mode Indicator[0x003C])
		0xA1, 0x02, //     Collection(Logical)
		0x19, 0x3D, //         UsageIdMin(Indicator On[0x003D])
		0x29, 0x41, //         UsageIdMax(Indicator Off[0x0041])
		0x15, 0x01, //         LogicalMinimum(1)
		0x25, 0x05, //         LogicalMaximum(5)
		0x95, 0x01, //         ReportCount(1)
		0x75, 0x03, //         ReportSize(3)
		0x91, 0x00, //         Output(Data, Array, Absolute, NoWrap, Linear, PreferredState, NoNullPosition, NonVolatile, BitField)
		0xC0,       //     EndCollection()
		0x75, 0x05, //     ReportSize(5)
		0x91, 0x03, //     Output(Constant, Variable, Absolute, NoWrap, Linear, PreferredState, NoNullPosition, NonVolatile, BitField)
		0xC0, // EndCollection()
	}

	b8Mega = []byte{
		0x05, 0x0C, // UsagePage(Consumer[0x000C])
		0x09, 0x01, // UsageId(Consumer Control[0x0001])
		0xA1, 0x01, // Collection(Application)
		0x85, 0x01, //     ReportId(1)
		0x09, 0x03, //     UsageId(Programmable Buttons[0x0003])
		0xA1, 0x02, //     Collection(Logical)
		0x05, 0x09, //         UsagePage(Button[0x0009])
		0x19, 0x01, //         UsageIdMin(Button 1[0x0001])
		0x29, 0x08, //         UsageIdMax(Button 8[0x0008])
		0x15, 0x00, //         LogicalMinimum(0)
		0x25, 0x01, //         LogicalMaximum(1)
		0x95, 0x08, //         ReportCount(8)
		0x75, 0x01, //         ReportSize(1)
		0x81, 0x02, //         Input(Data, Variable, Absolute, NoWrap, Linear, PreferredState, NoNullPosition, BitField)
		0xC0,       //     EndCollection()
		0x05, 0x08, //     UsagePage(LED[0x0008])
		0x09, 0x3C, //     UsageId(Usage Multi Mode Indicator[0x003C])
		0xA1, 0x02, //     Collection(Logical)
		0x19, 0x3D, //         UsageIdMin(Indicator On[0x003D])
		0x29, 0x41, //         UsageIdMax(Indicator Off[0x0041])
		0x15, 0x01, //         LogicalMinimum(1)
		0x25, 0x05, //         LogicalMaximum(5)
		0x95, 0x01, //         ReportCount(1)
		0x75, 0x03, //         ReportSize(3)
		0x91, 0x00, //         Output(Data, Array, Absolute, NoWrap, Linear, PreferredState, NoNullPosition, NonVolatile, BitField)
		0xC0,       //     EndCollection()
		0x75, 0x05, //     ReportSize(5)
		0x91, 0x03, //     Output(Constant, Variable, Absolute, NoWrap, Linear, PreferredState, NoNullPosition, NonVolatile, BitField)
		0x06, 0x00, 0xFF, //     UsagePage(b8[0xFF00])
		0x09, 0x01, //     UsageId(Capabilities[0x0001])
		0xA1, 0x02, //     Collection(Logical)
		0x09, 0x02, //         UsageId(With Display[0x0002])
		0x15, 0x00, //         LogicalMinimum(0)
		0x25, 0x01, //         LogicalMaximum(1)
		0x75, 0x01, //         ReportSize(1)
		0xB1, 0x03, //         Feature(Constant, Variable, Absolute, NoWrap, Linear, PreferredState, NoNullPosition, NonVolatile, BitField)
		0xC0,       //     EndCollection()
		0x75, 0x07, //     ReportSize(7)
		0xB1, 0x03, //     Feature(Constant, Variable, Absolute, NoWrap, Linear, PreferredState, NoNullPosition, NonVolatile, BitField)
		0x85, 0x02, //     ReportId(2)
		0x09, 0x11, //     UsageId(Display Capabilities[0x0011])
		0xA1, 0x02, //     Collection(Logical)
		0x09, 0x12, //         UsageId(Display Lines[0x0012])
		0x09, 0x13, //         UsageId(Display Characters per Line[0x0013])
		0x26, 0xFF, 0x00, //         LogicalMaximum(255)
		0x95, 0x02, //         ReportCount(2)
		0x75, 0x08, //         ReportSize(8)
		0xB1, 0x03, //         Feature(Constant, Variable, Absolute, NoWrap, Linear, PreferredState, NoNullPosition, NonVolatile, BitField)
		0xC0,       //     EndCollection()
		0x09, 0x21, //     UsageId(Display Data[0x0021])
		0xA1, 0x02, //     Collection(Logical)
		0x09, 0x22, //         UsageId(Line[0x0022])
		0x25, 0x1F, //         LogicalMaximum(31)
		0x95, 0x01, //         ReportCount(1)
		0x75, 0x05, //         ReportSize(5)
		0x91, 0x02, //         Output(Data, Variable, Absolute, NoWrap, Linear, PreferredState, NoNullPosition, NonVolatile, BitField)
		0x75, 0x03, //         ReportSize(3)
		0x91, 0x03, //         Output(Constant, Variable, Absolute, NoWrap, Linear, PreferredState, NoNullPosition, NonVolatile, BitField)
		0x19, 0x32, //         UsageIdMin(Alignment Left[0x0032])
		0x29, 0x34, //         UsageIdMax(Alignment Center[0x0034])
		0x15, 0x01, //         LogicalMinimum(1)
		0x25, 0x03, //         LogicalMaximum(3)
		0x75, 0x02, //         ReportSize(2)
		0x91, 0x00, //         Output(Data, Array, Absolute, NoWrap, Linear, PreferredState, NoNullPosition, NonVolatile, BitField)
		0x75, 0x06, //         ReportSize(6)
		0x91, 0x03, //         Output(Constant, Variable, Absolute, NoWrap, Linear, PreferredState, NoNullPosition, NonVolatile, BitField)
		0x09, 0x23, //         UsageId(Line Data[0x0023])
		0x15, 0x00, //         LogicalMinimum(0)
		0x26, 0xFF, 0x00, //         LogicalMaximum(255)
		0x95, 0x15, //         ReportCount(21)
		0x75, 0x08, //         ReportSize(8)
		0x91, 0x02, //         Output(Data, Variable, Absolute, NoWrap, Linear, PreferredState, NoNullPosition, NonVolatile, BitField)
		0xC0, //     EndCollection()
		0xC0, // EndCollection()
	}

	bPushPop = []byte{
		0x05, 0x01, // UsagePage(Generic Desktop[0x0001])
		0x09, 0x06, // UsageId(Keyboard[0x0006])
		0xA1, 0x01, // Collection(Application)
		0x75, 0x08, //     ReportSize(8)
		0x95, 0x04, //     ReportCount(4)
		0xA4,       //     Push()
		0x75, 0x01, //     ReportSize(1)
		0x95, 0x08, //     ReportCount(8)
		0x81, 0x02, //     Input(Data, Variable, Absolute, NoWrap, Linear, PreferredState, NoNullPosition, BitField)
		0xB4,       //     Pop()
		0x81, 0x02, //     Input(Data, Variable, Absolute, NoWrap, Linear, PreferredState, NoNullPosition, BitField)
		0xC0, // EndCollection()
	}

	bMultiTLC = []byte{
		0x05, 0x01, // UsagePage(Generic Desktop[0x0001])
		0x09, 0x06, // UsageId(Keyboard[0x0006])
		0xA1, 0x01, // Collection(Application)
		0x85, 0x01, //     ReportId(1)
		0x95, 0x08, //     ReportCount(8)
		0x75, 0x01, //     ReportSize(1)
		0x81, 0x02, //     Input(Data, Variable, Absolute, NoWrap, Linear, PreferredState, NoNullPosition, BitField)
		0xC0,       // EndCollection()
		0x05, 0x0C, // UsagePage(Consumer[0x000C])
		0x09, 0x01, // UsageId(Consumer Control[0x0001])
		0xA1, 0x01, // Collection(Application)
		0x85, 0x02, //     ReportId(2)
		0x95, 0x01, //     ReportCount(1)
		0x75, 0x10, //     ReportSize(16)
		0x81, 0x00, //     Input(Data, Array, Absolute, NoWrap, Linear, PreferredState, NoNullPosition, BitField)
		0xC0, // EndCollection()
	}

	bTruncated = []byte{
		0x05, 0x01, // UsagePage(Generic Desktop[0x0001])
		0x09, 0x06, // UsageId(Keyboard[0x0006])
		0xA1, 0x01, // Collection(Application)
		0x95, 0x08, //     ReportCount(8)
		0x75, 0x01, //     ReportSize(1)
		0x81, 0x02, //     Input(Data, Variable, Absolute, NoWrap, Linear, PreferredState, NoNullPosition, BitField)
		0xC0, // EndCollection()
		0x85, // ReportId(), truncated
	}

	bLongItem = []byte{
		0x05, 0x01, // UsagePage(Generic Desktop[0x0001])
		0x09, 0x06, // UsageId(Keyboard[0x0006])
		0xA1, 0x01, // Collection(Application)
		0x95, 0x01, //     ReportCount(1)
		0x75, 0x08, //     ReportSize(8)
		0xFE, 0x02, 0x01, 0x81, 0x02, //     LongItem(0x01), payload looks like Input()
		0x81, 0x02, //     Input(Data, Variable, Absolute, NoWrap, Linear, PreferredState, NoNullPosition, BitField)
		0xC0, // EndCollection()
	}

	args = []struct {
		descr      []byte
		rusagePage uint16
		rusage     uint16
		sinput     uint16
		soutput    uint16
		sfeature   uint16
		withId     bool
	}{
		{b8, 0x000C, 0x0001, 1, 1, 0, true},
		{b8Mega, 0x000C, 0x0001, 1, 23, 2, true},
		{bPushPop, 0x0001, 0x0006, 5, 0, 0, false},
		{bMultiTLC, 0x0001, 0x0006, 2, 0, 0, true},
		{bTruncated, 0x0001, 0x0006, 1, 0, 0, false},
		{bLongItem, 0x0001, 0x0006, 1, 0, 0, false},
	}
)

func TestParse(t *testing.T) {
	for i, tt := range args {
		rusagePage, rusage, sinput, soutput, sfeature, withId := hidParseReportDescriptor(tt.descr)

		if rusagePage != tt.rusagePage {
			t.Errorf("%d: bad usage page: got %q, want %q", i, rusagePage, tt.rusagePage)
		}
		if rusage != tt.rusage {
			t.Errorf("%d: bad usage: got %q, want %q", i, rusage, tt.rusage)
		}
		if sinput != tt.sinput {
			t.Errorf("%d: bad size input: got %d, want %d", i, sinput, tt.sinput)
		}
		if soutput != tt.soutput {
			t.Errorf("%d: bad size output: got %d, want %d", i, soutput, tt.soutput)
		}
		if sfeature != tt.sfeature {
			t.Errorf("%d: bad size feature: got %d, want %d", i, sfeature, tt.sfeature)
		}
		if withId != tt.withId {
			t.Errorf("%d: bad with id: got %v, want %v", i, withId, tt.withId)
		}
	}
}

func TestParseLedgerDescriptors(t *testing.T) {
	report := []byte{
		0x09, 0x01, // UsageId(1)
		0xA1, 0x01, // Collection(Application)
		0x09, 0x03, //     UsageId(3)
		0x15, 0x00, //     LogicalMinimum(0)
		0x26, 0xFF, 0x00, // LogicalMaximum(255)
		0x75, 0x08, //     ReportSize(8)
		0x95, 0x40, //     ReportCount(64)
		0x81, 0x08, //     Input
		0x09, 0x04, //     UsageId(4)
		0x15, 0x00, //     LogicalMinimum(0)
		0x26, 0xFF, 0x00, // LogicalMaximum(255)
		0x75, 0x08, //     ReportSize(8)
		0x95, 0x40, //     ReportCount(64)
		0x91, 0x08, //     Output
		0xC0, // EndCollection()
	}

	for _, tt := range []struct {
		name string
		page []byte
		want uint16
	}{
		{"apdu", []byte{0x06, 0xA0, 0xFF}, 0xffa0},
		{"fido", []byte{0x06, 0xD0, 0xF1}, 0xf1d0},
	} {
		page, usage, input, output, feature, withId := hidParseReportDescriptor(append(append([]byte{}, tt.page...), report...))
		if page != tt.want || usage != 1 || input != 64 || output != 64 || feature != 0 || withId {
			t.Errorf("%s: got page=0x%04x usage=%d input=%d output=%d feature=%d withId=%v", tt.name, page, usage, input, output, feature, withId)
		}
	}
}

func TestParseMalformedDescriptors(t *testing.T) {
	for _, tt := range []struct {
		name  string
		descr []byte
		page  uint16
		input uint16
	}{
		{
			name:  "count times size wraps to zero in 32 bits",
			descr: []byte{0x97, 0x00, 0x00, 0x00, 0x80, 0x77, 0x02, 0x00, 0x00, 0x00, 0x81, 0x00},
			input: 0xffff,
		},
		{
			name:  "maximum bit count",
			descr: []byte{0x97, 0xFF, 0xFF, 0xFF, 0xFF, 0x75, 0x01, 0x81, 0x00},
			input: 0xffff,
		},
		{
			name:  "large count times large size",
			descr: []byte{0x97, 0x00, 0x00, 0x01, 0x00, 0x77, 0x00, 0x00, 0x08, 0x00, 0x81, 0x00},
			input: 0xffff,
		},
		{
			name:  "largest length that fits",
			descr: []byte{0x96, 0xFF, 0xFF, 0x75, 0x08, 0x81, 0x00},
			input: 0xffff,
		},
		{
			name:  "one byte over the largest length",
			descr: []byte{0x97, 0x00, 0x00, 0x01, 0x00, 0x75, 0x08, 0x81, 0x00},
			input: 0xffff,
		},
		{
			name: "length accumulated over several items",
			descr: []byte{
				0x96, 0xFF, 0xFF, 0x75, 0x08, 0x81, 0x00,
				0x96, 0xFF, 0xFF, 0x75, 0x08, 0x81, 0x00,
			},
			input: 0xffff,
		},
		{
			name:  "usage page wider than 16 bits",
			descr: []byte{0x07, 0xA0, 0xFF, 0x01, 0x00},
		},
		{
			name:  "usage page wider than 16 bits keeps the previous one",
			descr: []byte{0x05, 0x01, 0x07, 0xA0, 0xFF, 0x01, 0x00},
			page:  0x0001,
		},
		{
			name:  "end collection without a collection",
			descr: []byte{0xC0, 0x05, 0x01, 0xA1, 0x01, 0x06, 0xA0, 0xFF, 0xA1, 0x01, 0xC0, 0xC0},
			page:  0x0001,
		},
		{
			name: "report ids are 8 bits wide",
			descr: []byte{
				0x85, 0x01, 0x95, 0x01, 0x75, 0x08, 0x81, 0x00,
				0x86, 0x01, 0x01, 0x95, 0x02, 0x75, 0x08, 0x81, 0x00,
			},
			input: 3,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			page, _, input, _, _, _ := hidParseReportDescriptor(tt.descr)
			if page != tt.page {
				t.Errorf("usage page: got 0x%04x, want 0x%04x", page, tt.page)
			}
			if input != tt.input {
				t.Errorf("input length: got %d, want %d", input, tt.input)
			}
		})
	}
}

func TestParseExtendedUsage(t *testing.T) {
	for _, tt := range []struct {
		name  string
		descr []byte
		page  uint16
		usage uint16
	}{
		{
			name:  "the usage carries the page",
			descr: []byte{0x05, 0x01, 0x0B, 0x01, 0x00, 0xA0, 0xFF, 0xA1, 0x01},
			page:  0xffa0,
			usage: 0x0001,
		},
		{
			name:  "a later usage page does not replace it",
			descr: []byte{0x0B, 0x01, 0x00, 0xA0, 0xFF, 0x05, 0x01, 0xA1, 0x01},
			page:  0xffa0,
			usage: 0x0001,
		},
		{
			name:  "a later short usage uses the usage page",
			descr: []byte{0x0B, 0x01, 0x00, 0xA0, 0xFF, 0x05, 0x01, 0x09, 0x02, 0xA1, 0x01},
			page:  0x0001,
			usage: 0x0002,
		},
		{
			name:  "inside a collection it is ignored",
			descr: []byte{0x05, 0x01, 0x09, 0x06, 0xA1, 0x01, 0x0B, 0x01, 0x00, 0xA0, 0xFF, 0xC0},
			page:  0x0001,
			usage: 0x0006,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			page, usage, _, _, _, _ := hidParseReportDescriptor(tt.descr)
			if page != tt.page || usage != tt.usage {
				t.Errorf("got 0x%04x/0x%04x, want 0x%04x/0x%04x", page, usage, tt.page, tt.usage)
			}
		})
	}
}

func TestParsePopRestoresUsagePage(t *testing.T) {
	for _, tt := range []struct {
		name  string
		descr []byte
		page  uint16
	}{
		{
			name:  "pop restores the page before a collection",
			descr: []byte{0x06, 0xA0, 0xFF, 0xA4, 0x05, 0x01, 0xB4, 0x09, 0x01, 0xA1, 0x01, 0xC0},
			page:  0xffa0,
		},
		{
			name:  "pop inside a collection does not change it",
			descr: []byte{0x05, 0x01, 0xA4, 0x06, 0xA0, 0xFF, 0x09, 0x01, 0xA1, 0x01, 0xB4, 0xC0},
			page:  0xffa0,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if page, _, _, _, _, _ := hidParseReportDescriptor(tt.descr); page != tt.page {
				t.Errorf("got 0x%04x, want 0x%04x", page, tt.page)
			}
		})
	}
}
