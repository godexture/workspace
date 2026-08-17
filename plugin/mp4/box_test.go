package mp4

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"testing"

	"github.com/godexture/godec/access"
)

func TestScanTopLevelBoxes(t *testing.T) {
	userType := [16]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	data := appendBox(nil, 12, "ftyp", []byte{1, 2, 3, 4})
	data = appendLargeBox(data, 20, "free", []byte{5, 6, 7, 8})
	data = appendUUIDBox(data, 24, userType, nil)
	data = appendLargeUUIDBox(data, 32, userType, nil)
	data = appendSizeZeroBox(data, "mdat", []byte{9, 10, 11})

	values := collectTopLevel(t, data)
	if len(values) != 5 {
		t.Fatalf("box count = %d, want 5", len(values))
	}
	wantTypes := []string{"ftyp", "free", "uuid", "uuid", "mdat"}
	wantOffsets := []uint64{0, 12, 32, 56, 88}
	wantSizes := []uint64{12, 20, 24, 32, 11}
	for index, value := range values {
		if got := string(value.typeID[:]); got != wantTypes[index] || value.offset != wantOffsets[index] || value.size != wantSizes[index] {
			t.Fatalf("box %d = {%q %d %d}, want {%q %d %d}", index, got, value.offset, value.size, wantTypes[index], wantOffsets[index], wantSizes[index])
		}
	}
	if values[1].headerSize != 16 || values[1].payloadOffset != 28 || values[1].payloadSize != 4 {
		t.Fatalf("large box range = %#v", values[1])
	}
	if !values[2].hasUserType || values[2].userType != userType || values[2].headerSize != 24 {
		t.Fatalf("uuid box = %#v", values[2])
	}
	if !values[3].hasUserType || values[3].userType != userType || values[3].headerSize != 32 {
		t.Fatalf("large uuid box = %#v", values[3])
	}
}

func TestScanChildBoxes(t *testing.T) {
	childA := appendBox(nil, 8, "free", nil)
	childB := appendBox(nil, 8, "skip", nil)
	data := appendBox(nil, 8+len(childA)+len(childB), "moov", append(childA, childB...))

	topLevel := collectTopLevel(t, data)
	var children []box
	err := scanChildBoxes(context.Background(), memoryRandom(data), uint64(len(data)), topLevel[0], func(value box) error {
		children = append(children, value)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 2 || string(children[0].typeID[:]) != "free" || string(children[1].typeID[:]) != "skip" {
		t.Fatalf("children = %#v", children)
	}
}

func TestScanBoxesKeepsUnknownRangesOpaqueAndOrdered(t *testing.T) {
	data := appendBox(nil, 11, "zzzz", []byte{1, 2, 3})
	data = appendBox(data, 10, "abcd", []byte{4, 5})
	reader := &recordingRandom{data: data}
	var values []box
	err := scanTopLevelBoxes(context.Background(), reader, uint64(len(data)), func(value box) error {
		values = append(values, value)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 || string(values[0].typeID[:]) != "zzzz" || string(values[1].typeID[:]) != "abcd" {
		t.Fatalf("boxes = %#v", values)
	}
	if values[0].payloadOffset != 8 || values[0].payloadSize != 3 || values[1].payloadOffset != 19 || values[1].payloadSize != 2 {
		t.Fatalf("payload ranges = %#v", values)
	}
	if reader.largestRead != 8 {
		t.Fatalf("scanner read %d payload bytes, want only headers", reader.largestRead)
	}
}

func TestScanBoxesRejectsMalformedRanges(t *testing.T) {
	tests := []struct {
		name  string
		data  []byte
		scope boxScope
	}{
		{
			name:  "smaller than header",
			data:  appendBox(nil, 7, "free", nil),
			scope: boxScope{sourceEnd: 8, end: 8, topLevel: true},
		},
		{
			name:  "large size smaller than header",
			data:  appendLargeBox(nil, 15, "free", nil),
			scope: boxScope{sourceEnd: 16, end: 16, topLevel: true},
		},
		{
			name:  "large size zero",
			data:  appendLargeBox(nil, 0, "free", nil),
			scope: boxScope{sourceEnd: 16, end: 16, topLevel: true},
		},
		{
			name:  "uuid smaller than header",
			data:  appendUUIDBox(nil, 23, [16]byte{}, nil),
			scope: boxScope{sourceEnd: 24, end: 24, topLevel: true},
		},
		{
			name:  "outside source",
			data:  appendBox(nil, 16, "free", []byte{1, 2, 3, 4}),
			scope: boxScope{sourceEnd: 12, end: 12, topLevel: true},
		},
		{
			name:  "outside parent",
			data:  appendBox(nil, 16, "free", []byte{1, 2, 3, 4, 5, 6, 7, 8}),
			scope: boxScope{sourceEnd: 16, end: 12, topLevel: true},
		},
		{
			name:  "nested zero size",
			data:  appendSizeZeroBox(nil, "free", nil),
			scope: boxScope{sourceEnd: 8, end: 8},
		},
		{
			name: "overflow",
			data: append(make([]byte, 8), appendLargeBox(nil, math.MaxUint64, "free", nil)...),
			scope: boxScope{
				sourceEnd: math.MaxUint64,
				start:     8,
				end:       math.MaxUint64,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := scanBoxes(context.Background(), memoryRandom(test.data), test.scope, func(box) error { return nil })
			if !errors.Is(err, errMalformedBox) {
				t.Fatalf("scanBoxes() error = %v, want malformed box", err)
			}
		})
	}
}

func TestScanBoxesRejectsTruncatedAndNoProgressReads(t *testing.T) {
	data := appendBox(nil, 8, "free", nil)
	for _, reader := range []access.Random{
		memoryRandom(data[:4]),
		noProgressRandom{},
	} {
		err := scanTopLevelBoxes(context.Background(), reader, uint64(len(data)), func(box) error { return nil })
		if !errors.Is(err, errMalformedBox) {
			t.Fatalf("scanTopLevelBoxes() error = %v, want malformed box", err)
		}
	}
}

func TestScanBoxesHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := scanTopLevelBoxes(ctx, memoryRandom(appendBox(nil, 8, "free", nil)), 8, func(box) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("scanTopLevelBoxes() error = %v, want context canceled", err)
	}
}

func collectTopLevel(t *testing.T, data []byte) []box {
	t.Helper()
	var values []box
	err := scanTopLevelBoxes(context.Background(), memoryRandom(data), uint64(len(data)), func(value box) error {
		values = append(values, value)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return values
}

func appendBox(destination []byte, size int, typeID string, payload []byte) []byte {
	var header [8]byte
	binary.BigEndian.PutUint32(header[:4], uint32(size))
	copy(header[4:], typeID)
	return append(append(destination, header[:]...), payload...)
}

func appendLargeBox(destination []byte, size uint64, typeID string, payload []byte) []byte {
	var header [16]byte
	binary.BigEndian.PutUint32(header[:4], 1)
	copy(header[4:8], typeID)
	binary.BigEndian.PutUint64(header[8:], size)
	return append(append(destination, header[:]...), payload...)
}

func appendUUIDBox(destination []byte, size int, userType [16]byte, payload []byte) []byte {
	var header [24]byte
	binary.BigEndian.PutUint32(header[:4], uint32(size))
	copy(header[4:8], "uuid")
	copy(header[8:], userType[:])
	return append(append(destination, header[:]...), payload...)
}

func appendLargeUUIDBox(destination []byte, size uint64, userType [16]byte, payload []byte) []byte {
	var header [32]byte
	binary.BigEndian.PutUint32(header[:4], 1)
	copy(header[4:8], "uuid")
	binary.BigEndian.PutUint64(header[8:16], size)
	copy(header[16:], userType[:])
	return append(append(destination, header[:]...), payload...)
}

func appendSizeZeroBox(destination []byte, typeID string, payload []byte) []byte {
	var header [8]byte
	copy(header[4:], typeID)
	return append(append(destination, header[:]...), payload...)
}

type memoryRandom []byte

func (r memoryRandom) ReadAt(ctx context.Context, destination []byte, offset int64) (int, error) {
	if err := context.Cause(ctx); err != nil {
		return 0, err
	}
	if offset < 0 || offset >= int64(len(r)) {
		return 0, io.EOF
	}
	count := copy(destination, r[offset:])
	if count < len(destination) {
		return count, io.EOF
	}
	return count, nil
}

type recordingRandom struct {
	data        []byte
	largestRead int
}

func (r *recordingRandom) ReadAt(ctx context.Context, destination []byte, offset int64) (int, error) {
	if len(destination) > r.largestRead {
		r.largestRead = len(destination)
	}
	return memoryRandom(r.data).ReadAt(ctx, destination, offset)
}

type noProgressRandom struct{}

func (noProgressRandom) ReadAt(context.Context, []byte, int64) (int, error) { return 0, nil }
