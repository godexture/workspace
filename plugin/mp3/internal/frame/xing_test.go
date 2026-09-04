package frame

import (
	"encoding/binary"
	"errors"
	"testing"
)

func TestParseXingUsesDerivedSideInfoPlacement(t *testing.T) {
	tests := []struct {
		name      string
		versionID byte
		mono      bool
		crc       bool
		offset    int
	}{
		{name: "mpeg1 stereo", versionID: 3, offset: 36},
		{name: "mpeg1 mono", versionID: 3, mono: true, offset: 21},
		{name: "mpeg2 stereo", versionID: 2, offset: 21},
		{name: "mpeg2 mono", versionID: 2, mono: true, offset: 13},
		{name: "mpeg25 stereo", versionID: 0, offset: 21},
		{name: "crc remains at side-info offset", versionID: 3, crc: true, offset: 36},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := testHeader(test.versionID, 1, 9, 0, false, 0)
			if test.mono {
				raw[3] = byte(Mono) << 6
			}
			if test.crc {
				raw[1] &^= 0x01
			}
			header, err := Parse(raw[:])
			if err != nil {
				t.Fatal(err)
			}

			data := make([]byte, test.offset+120)
			copy(data[:4], raw[:])
			copy(data[test.offset:], []byte("Xing"))
			binary.BigEndian.PutUint32(data[test.offset+4:], 0x0000000f)
			binary.BigEndian.PutUint32(data[test.offset+8:], 123)
			binary.BigEndian.PutUint32(data[test.offset+12:], 456789)
			for i := range data[test.offset+16 : test.offset+116] {
				data[test.offset+16+i] = byte(i)
			}
			binary.BigEndian.PutUint32(data[test.offset+116:], 77)

			got, found, err := ParseXing(header, data)
			if err != nil || !found {
				t.Fatalf("ParseXing() = %+v, %v, %v", got, found, err)
			}
			if got.Kind() != XingKindXing || got.Flags() != 0x0000000f {
				t.Fatalf("marker/flags = %v/%08x", got.Kind(), got.Flags())
			}
			if !got.HasFrames() || got.Frames() != 123 || !got.HasBytes() || got.Bytes() != 456789 {
				t.Fatalf("frames/bytes = %d/%d", got.Frames(), got.Bytes())
			}
			if !got.HasTOC() || got.TOC()[99] != 99 || !got.HasQuality() || got.Quality() != 77 {
				t.Fatalf("toc/quality were not decoded")
			}
		})
	}
}

func TestParseXingRecognizesInfo(t *testing.T) {
	raw := testHeader(3, 1, 9, 0, false, 0)
	header, err := Parse(raw[:])
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 44)
	copy(data[:4], raw[:])
	copy(data[36:], []byte("Info"))
	binary.BigEndian.PutUint32(data[40:], 0)

	got, found, err := ParseXing(header, data)
	if err != nil || !found || got.Kind() != XingKindInfo {
		t.Fatalf("ParseXing() = %+v, %v, %v", got, found, err)
	}
}

func TestParseXingDoesNotTrustFixedStereoOffset(t *testing.T) {
	raw := testHeader(3, 1, 9, 0, false, 0)
	raw[3] = byte(Mono) << 6
	header, err := Parse(raw[:])
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 40)
	copy(data[:4], raw[:])
	copy(data[36:], []byte("Xing"))

	if _, found, err := ParseXing(header, data); err != nil || found {
		t.Fatalf("ParseXing() = %v, %v, want marker at wrong offset ignored", found, err)
	}
}

func TestParseXingRejectsShortAndTruncatedIndexes(t *testing.T) {
	raw := testHeader(3, 1, 9, 0, false, 0)
	header, err := Parse(raw[:])
	if err != nil {
		t.Fatal(err)
	}

	short := make([]byte, 39)
	if _, found, err := ParseXing(header, short); found || err != nil {
		t.Fatalf("short ParseXing() = %v, %v, want absent", found, err)
	}

	truncated := make([]byte, 44)
	copy(truncated[36:], []byte("Xing"))
	binary.BigEndian.PutUint32(truncated[40:], 1)
	if _, found, err := ParseXing(header, truncated); !found || !errors.Is(err, ErrIndexTooShort) {
		t.Fatalf("truncated ParseXing() = %v, %v, want found ErrIndexTooShort", found, err)
	}
}

func TestParseXingTreatsShortValidMP3FrameWithoutIndexAsAbsent(t *testing.T) {
	raw := testHeader(2, 1, 1, 0, false, 0)
	header, err := Parse(raw[:])
	if err != nil {
		t.Fatal(err)
	}
	data := testFrame(raw, 26)

	if _, found, err := ParseXing(header, data); found || err != nil {
		t.Fatalf("ParseXing() = %v, %v, want absent short frame", found, err)
	}
}

func TestParseXingTreatsAbsentMarkerAsAbsent(t *testing.T) {
	raw := testHeader(2, 1, 9, 0, false, 0)
	header, err := Parse(raw[:])
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 25)
	copy(data[:4], raw[:])

	got, found, err := ParseXing(header, data)
	if err != nil || found || got.Kind() != 0 {
		t.Fatalf("ParseXing() = %+v, %v, %v, want absent", got, found, err)
	}
}

func TestParseXingRejectsInvalidHeader(t *testing.T) {
	if _, found, err := ParseXing(Header{}, make([]byte, 128)); found || !errors.Is(err, ErrInvalidHeader) {
		t.Fatalf("ParseXing(invalid header) = %v, %v", found, err)
	}
}
