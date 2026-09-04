package frame

import (
	"encoding/binary"
	"errors"
	"slices"
	"testing"
)

func TestParseVBRIDecodesBoundedTOC(t *testing.T) {
	for entrySize := 1; entrySize <= 4; entrySize++ {
		t.Run(string(rune('0'+entrySize)), func(t *testing.T) {
			data := makeVBRI(entrySize, 3, 2, []uint32{1, 2, 3})
			header := validMP3Header(t)
			got, found, err := ParseVBRI(header, data)
			if err != nil || !found {
				t.Fatalf("ParseVBRI() = %+v, %v, %v", got, found, err)
			}
			if got.Version() != 1 || got.Delay() != 2 || got.Quality() != 75 || got.Bytes() != 45000 || got.Frames() != 7344 {
				t.Fatalf("fixed fields = version %d delay %d quality %d bytes %d frames %d", got.Version(), got.Delay(), got.Quality(), got.Bytes(), got.Frames())
			}
			if got.EntryCount() != 3 || got.Scale() != 2 || got.EntrySize() != uint16(entrySize) || got.FramesPerEntry() != 845 {
				t.Fatalf("table fields = count %d scale %d size %d frames/entry %d", got.EntryCount(), got.Scale(), got.EntrySize(), got.FramesPerEntry())
			}
			wantTOC := []uint32{1, 2, 3}
			if gotTOC := got.TOC(); !slices.Equal(gotTOC, wantTOC) {
				t.Fatalf("TOC = %v, want %v", gotTOC, wantTOC)
			}
			copyOfTOC := got.TOC()
			copyOfTOC[0] = 999
			if got.TOC()[0] != wantTOC[0] {
				t.Fatal("TOC accessor exposed mutable parser state")
			}
		})
	}
}

func TestParseVBRIUsesFixedOffset(t *testing.T) {
	data := make([]byte, 62)
	copy(data[36:], []byte("VBRI"))
	binary.BigEndian.PutUint16(data[40:], 1)
	binary.BigEndian.PutUint16(data[58:], 1)
	header := validMP3Header(t)
	if _, found, err := ParseVBRI(header, data); err != nil || !found {
		t.Fatalf("ParseVBRI() = %v, %v, want fixed-offset marker", found, err)
	}
}

func TestParseVBRIRejectsShortAndTruncatedInput(t *testing.T) {
	header := validMP3Header(t)
	shortRaw := testHeader(2, 1, 1, 0, false, 0)
	shortHeader, err := Parse(shortRaw[:])
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := ParseVBRI(shortHeader, testFrame(shortRaw, 26)); found || err != nil {
		t.Fatalf("short ParseVBRI() = %v, %v, want absent", found, err)
	}

	data := makeVBRI(2, 2, 1, []uint32{1, 2})
	data = data[:len(data)-1]
	if _, found, err := ParseVBRI(header, data); !found || !errors.Is(err, ErrIndexTooShort) {
		t.Fatalf("truncated TOC ParseVBRI() = %v, %v, want found ErrIndexTooShort", found, err)
	}
}

func TestParseVBRIRejectsInvalidEntrySize(t *testing.T) {
	header := validMP3Header(t)
	for _, entrySize := range []uint16{0, 5} {
		data := make([]byte, 62)
		copy(data[36:], []byte("VBRI"))
		binary.BigEndian.PutUint16(data[40:], 1)
		binary.BigEndian.PutUint16(data[58:], entrySize)
		if _, found, err := ParseVBRI(header, data); !found || !errors.Is(err, ErrIndexInvalid) {
			t.Fatalf("entry size %d: ParseVBRI() = %v, %v, want ErrIndexInvalid", entrySize, found, err)
		}
	}
}

func TestParseVBRIRetainsRawTOCAndScale(t *testing.T) {
	data := makeVBRI(4, 2, 65535, []uint32{1, 0xffffffff})
	header := validMP3Header(t)
	got, found, err := ParseVBRI(header, data)
	if err != nil || !found {
		t.Fatalf("ParseVBRI() = %+v, %v, %v", got, found, err)
	}
	if got.Scale() != 65535 || !slices.Equal(got.TOC(), []uint32{1, 0xffffffff}) {
		t.Fatalf("scale/TOC = %d/%v, want raw values", got.Scale(), got.TOC())
	}
}

func TestParseVBRITreatsAbsentMarkerAsAbsent(t *testing.T) {
	data := make([]byte, 62)
	header := validMP3Header(t)
	if _, found, err := ParseVBRI(header, data); found || err != nil {
		t.Fatalf("ParseVBRI() = %v, %v, want absent", found, err)
	}
}

func TestParseVBRIRejectsInvalidHeaderLayerAndVersion(t *testing.T) {
	data := makeVBRI(1, 1, 1, []uint32{1})
	if _, found, err := ParseVBRI(Header{}, data); found || !errors.Is(err, ErrInvalidHeader) {
		t.Fatalf("invalid header ParseVBRI() = %v, %v", found, err)
	}

	layerI := testHeader(3, 3, 1, 0, false, 0)
	layerIHeader, err := Parse(layerI[:])
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := ParseVBRI(layerIHeader, data); found || err != nil {
		t.Fatalf("Layer I ParseVBRI() = %v, %v, want absent", found, err)
	}

	badVersion := data
	binary.BigEndian.PutUint16(badVersion[40:], 2)
	header := validMP3Header(t)
	if _, found, err := ParseVBRI(header, badVersion); !found || !errors.Is(err, ErrIndexInvalid) {
		t.Fatalf("VBRI version ParseVBRI() = %v, %v, want ErrIndexInvalid", found, err)
	}
}

func makeVBRI(entrySize, entries int, scale uint16, values []uint32) []byte {
	data := make([]byte, 62+entrySize*entries)
	copy(data[36:], []byte("VBRI"))
	binary.BigEndian.PutUint16(data[40:], 1)
	binary.BigEndian.PutUint16(data[42:], 2)
	binary.BigEndian.PutUint16(data[44:], 75)
	binary.BigEndian.PutUint32(data[46:], 45000)
	binary.BigEndian.PutUint32(data[50:], 7344)
	binary.BigEndian.PutUint16(data[54:], uint16(entries))
	binary.BigEndian.PutUint16(data[56:], scale)
	binary.BigEndian.PutUint16(data[58:], uint16(entrySize))
	binary.BigEndian.PutUint16(data[60:], 845)
	offset := 62
	for _, value := range values {
		switch entrySize {
		case 1:
			data[offset] = byte(value)
		case 2:
			binary.BigEndian.PutUint16(data[offset:], uint16(value))
		case 3:
			data[offset] = byte(value >> 16)
			data[offset+1] = byte(value >> 8)
			data[offset+2] = byte(value)
		case 4:
			binary.BigEndian.PutUint32(data[offset:], value)
		}
		offset += entrySize
	}
	return data
}

func validMP3Header(t *testing.T) Header {
	t.Helper()
	raw := testHeader(3, 1, 9, 0, false, 0)
	header, err := Parse(raw[:])
	if err != nil {
		t.Fatal(err)
	}
	return header
}
