package frame

import (
	"errors"
	"testing"
)

func TestParseDerivesHeaderFields(t *testing.T) {
	tests := []struct {
		name       string
		versionID  byte
		layerID    byte
		bitrate    byte
		rate       byte
		padding    bool
		wantVer    Version
		wantLayer  Layer
		wantKbps   int
		wantRate   int
		wantSample int
		wantSize   int
	}{
		{
			name:       "mpeg1 layer1",
			versionID:  3,
			layerID:    3,
			bitrate:    1,
			rate:       0,
			wantVer:    VersionMPEG1,
			wantLayer:  LayerI,
			wantKbps:   32,
			wantRate:   44100,
			wantSample: 384,
			wantSize:   32,
		},
		{
			name:       "mpeg1 layer2",
			versionID:  3,
			layerID:    2,
			bitrate:    8,
			rate:       1,
			wantVer:    VersionMPEG1,
			wantLayer:  LayerII,
			wantKbps:   128,
			wantRate:   48000,
			wantSample: 1152,
			wantSize:   384,
		},
		{
			name:       "mpeg1 layer3 padded",
			versionID:  3,
			layerID:    1,
			bitrate:    9,
			rate:       0,
			padding:    true,
			wantVer:    VersionMPEG1,
			wantLayer:  LayerIII,
			wantKbps:   128,
			wantRate:   44100,
			wantSample: 1152,
			wantSize:   418,
		},
		{
			name:       "mpeg2 layer3",
			versionID:  2,
			layerID:    1,
			bitrate:    1,
			rate:       0,
			wantVer:    VersionMPEG2,
			wantLayer:  LayerIII,
			wantKbps:   8,
			wantRate:   22050,
			wantSample: 576,
			wantSize:   26,
		},
		{
			name:       "mpeg25 layer3",
			versionID:  0,
			layerID:    1,
			bitrate:    14,
			rate:       2,
			wantVer:    VersionMPEG25,
			wantLayer:  LayerIII,
			wantKbps:   160,
			wantRate:   8000,
			wantSample: 576,
			wantSize:   1440,
		},
		{
			name:       "mpeg2 layer1 padded",
			versionID:  2,
			layerID:    3,
			bitrate:    4,
			rate:       0,
			padding:    true,
			wantVer:    VersionMPEG2,
			wantLayer:  LayerI,
			wantKbps:   64,
			wantRate:   22050,
			wantSample: 384,
			wantSize:   140,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := testHeader(test.versionID, test.layerID, test.bitrate, test.rate, test.padding, 0)
			header, err := Parse(raw[:])
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if !header.Valid() {
				t.Fatal("parsed header is not valid")
			}
			if got := header.Version(); got != test.wantVer {
				t.Errorf("Version() = %v, want %v", got, test.wantVer)
			}
			if got := header.Layer(); got != test.wantLayer {
				t.Errorf("Layer() = %v, want %v", got, test.wantLayer)
			}
			if got := header.BitrateKbps(); got != test.wantKbps {
				t.Errorf("BitrateKbps() = %d, want %d", got, test.wantKbps)
			}
			if got := header.SampleRateHz(); got != test.wantRate {
				t.Errorf("SampleRateHz() = %d, want %d", got, test.wantRate)
			}
			if got := header.SamplesPerFrame(); got != test.wantSample {
				t.Errorf("SamplesPerFrame() = %d, want %d", got, test.wantSample)
			}
			if got, err := header.FrameSize(0); err != nil || got != test.wantSize {
				t.Errorf("FrameSize(0) = %d, %v, want %d", got, err, test.wantSize)
			}
			if got := header.PaddingBytes(); got != boolInt(test.padding)*map[Layer]int{LayerI: 4, LayerII: 1, LayerIII: 1}[test.wantLayer] {
				t.Errorf("PaddingBytes() = %d", got)
			}
		})
	}
}

func TestParseRejectsReservedAndMalformedFields(t *testing.T) {
	base := testHeader(3, 1, 9, 0, false, 0)
	tests := []struct {
		name string
		edit func(*[4]byte)
	}{
		{name: "short", edit: nil},
		{name: "sync byte", edit: func(raw *[4]byte) { raw[0] = 0xfe }},
		{name: "sync continuation", edit: func(raw *[4]byte) { raw[1] &^= 0x20 }},
		{name: "reserved version", edit: func(raw *[4]byte) { raw[1] = (raw[1] &^ 0x18) | 0x08 }},
		{name: "reserved layer", edit: func(raw *[4]byte) { raw[1] &^= 0x06 }},
		{name: "reserved bitrate", edit: func(raw *[4]byte) { raw[2] = (raw[2] & 0x0f) | 0xf0 }},
		{name: "reserved sample rate", edit: func(raw *[4]byte) { raw[2] = (raw[2] &^ 0x0c) | 0x0c }},
		{name: "reserved emphasis", edit: func(raw *[4]byte) { raw[3] = (raw[3] &^ 0x03) | 0x02 }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.edit == nil {
				_, err := Parse(base[:3])
				if !errors.Is(err, ErrHeaderTooShort) {
					t.Fatalf("Parse() error = %v, want ErrHeaderTooShort", err)
				}
				return
			}
			raw := base
			test.edit(&raw)
			if _, err := Parse(raw[:]); !errors.Is(err, ErrInvalidHeader) {
				t.Fatalf("Parse() error = %v, want ErrInvalidHeader", err)
			}
		})
	}
}

func TestFreeFormatFrameSizeAndCompatibility(t *testing.T) {
	raw := testHeader(3, 1, 0, 0, true, 0)
	header, err := Parse(raw[:])
	if err != nil {
		t.Fatal(err)
	}
	if !header.FreeFormat() || header.BitrateKbps() != 0 {
		t.Fatal("free-format header was not identified")
	}
	if _, err := header.FrameSize(0); !errors.Is(err, ErrFreeFormatSize) {
		t.Fatalf("FrameSize(0) error = %v, want ErrFreeFormatSize", err)
	}
	if got, err := header.FrameSize(300); err != nil || got != 301 {
		t.Fatalf("FrameSize(300) = %d, %v, want 301", got, err)
	}
	if _, err := header.FrameSize(3); !errors.Is(err, ErrFrameSize) {
		t.Fatalf("FrameSize(3) error = %v, want ErrFrameSize", err)
	}

	otherRaw := testHeader(3, 1, 0, 0, false, 3)
	other, err := Parse(otherRaw[:])
	if err != nil {
		t.Fatal(err)
	}
	if !header.Compatible(other) {
		t.Fatal("compatible free-format headers did not match")
	}

	cbrRaw := testHeader(3, 1, 9, 0, false, 0)
	cbr, err := Parse(cbrRaw[:])
	if err != nil {
		t.Fatal(err)
	}
	differentBitrateRaw := testHeader(3, 1, 10, 0, false, 0)
	differentBitrate, err := Parse(differentBitrateRaw[:])
	if err != nil {
		t.Fatal(err)
	}
	if !cbr.Compatible(differentBitrate) {
		t.Fatal("different bitrate headers should share stream framing")
	}

	differentRateRaw := testHeader(3, 1, 9, 1, false, 0)
	differentRate, err := Parse(differentRateRaw[:])
	if err != nil {
		t.Fatal(err)
	}
	if cbr.Compatible(differentRate) {
		t.Fatal("different sample-rate headers matched")
	}
}

func testHeader(versionID, layerID, bitrate, rate byte, padding bool, emphasis byte) [4]byte {
	var raw [4]byte
	raw[0] = 0xff
	raw[1] = 0xe0 | versionID<<3 | layerID<<1 | 0x01
	raw[2] = bitrate<<4 | rate<<2
	if padding {
		raw[2] |= 0x02
	}
	raw[3] = emphasis
	return raw
}
