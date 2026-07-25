package encoder

import (
	"encoding/binary"
	"testing"
)

func TestDeinterleaveS8(t *testing.T) {
	t.Parallel()
	// 2 channels, 3 samples: L/R = (-128,127), (-64,64), (0,0).
	plane := []byte{128, 127, 192, 64, 0, 0}
	buffer := [][]int64{make([]int64, 3), make([]int64, 3)}
	if err := deinterleaveS8(buffer, plane, 0, 3, 2, -128, 127, 8); err != nil {
		t.Fatalf("deinterleaveS8() error = %v", err)
	}
	wantLeft := []int64{-128, -64, 0}
	wantRight := []int64{127, 64, 0}
	for i := range wantLeft {
		if buffer[0][i] != wantLeft[i] || buffer[1][i] != wantRight[i] {
			t.Fatalf("sample %d = (%d, %d), want (%d, %d)", i, buffer[0][i], buffer[1][i], wantLeft[i], wantRight[i])
		}
	}
}

func TestDeinterleaveS8RejectsOutOfRange(t *testing.T) {
	t.Parallel()
	// bitsPerSample=4 restricts the valid signed range to [-8, 7]; signed
	// value 100 falls outside it.
	plane := []byte{100}
	buffer := [][]int64{make([]int64, 1)}
	if err := deinterleaveS8(buffer, plane, 0, 1, 1, -8, 7, 4); err == nil {
		t.Fatal("deinterleaveS8() error = nil, want out-of-range error")
	}
}

func TestDeinterleaveS16(t *testing.T) {
	t.Parallel()
	left := []int64{-32768, 0, 32767}
	right := []int64{32767, 0, -32768}
	plane := make([]byte, 12)
	for i := range left {
		binary.LittleEndian.PutUint16(plane[i*4:], uint16(int16(left[i])))
		binary.LittleEndian.PutUint16(plane[i*4+2:], uint16(int16(right[i])))
	}
	buffer := [][]int64{make([]int64, 3), make([]int64, 3)}
	if err := deinterleaveS16(buffer, plane, 0, 3, 2, -32768, 32767, 16); err != nil {
		t.Fatalf("deinterleaveS16() error = %v", err)
	}
	for i := range left {
		if buffer[0][i] != left[i] || buffer[1][i] != right[i] {
			t.Fatalf("sample %d = (%d, %d), want (%d, %d)", i, buffer[0][i], buffer[1][i], left[i], right[i])
		}
	}
}

func TestDeinterleaveS16RejectsOutOfRange(t *testing.T) {
	t.Parallel()
	// bitsPerSample=8 restricts the valid signed range to [-128, 127]; 200
	// falls outside it.
	plane := make([]byte, 2)
	binary.LittleEndian.PutUint16(plane, uint16(int16(200)))
	buffer := [][]int64{make([]int64, 1)}
	if err := deinterleaveS16(buffer, plane, 0, 1, 1, -128, 127, 8); err == nil {
		t.Fatal("deinterleaveS16() error = nil, want out-of-range error")
	}
}

func putS24(plane []byte, offset int, value int64) {
	u := uint32(value) & 0xffffff
	plane[offset] = byte(u)
	plane[offset+1] = byte(u >> 8)
	plane[offset+2] = byte(u >> 16)
}

func TestDeinterleaveS24(t *testing.T) {
	t.Parallel()
	left := []int64{-8388608, 0, 8388607}
	right := []int64{8388607, 0, -8388608}
	plane := make([]byte, 18)
	for i := range left {
		putS24(plane, i*6, left[i])
		putS24(plane, i*6+3, right[i])
	}
	buffer := [][]int64{make([]int64, 3), make([]int64, 3)}
	if err := deinterleaveS24(buffer, plane, 0, 3, 2, -8388608, 8388607, 24); err != nil {
		t.Fatalf("deinterleaveS24() error = %v", err)
	}
	for i := range left {
		if buffer[0][i] != left[i] || buffer[1][i] != right[i] {
			t.Fatalf("sample %d = (%d, %d), want (%d, %d)", i, buffer[0][i], buffer[1][i], left[i], right[i])
		}
	}
}

func TestDeinterleaveS24RejectsOutOfRange(t *testing.T) {
	t.Parallel()
	// bitsPerSample=8 restricts the valid signed range to [-128, 127]; 1000
	// falls outside it.
	plane := make([]byte, 3)
	putS24(plane, 0, 1000)
	buffer := [][]int64{make([]int64, 1)}
	if err := deinterleaveS24(buffer, plane, 0, 1, 1, -128, 127, 8); err == nil {
		t.Fatal("deinterleaveS24() error = nil, want out-of-range error")
	}
}

func TestDeinterleaveS32(t *testing.T) {
	t.Parallel()
	// 4 samples so this also exercises the SIMD stereo path when available
	// (deinterleaveS32 dispatches to it for channels==2 && samples>=4).
	left := []int64{-2147483648, -1, 0, 2147483647}
	right := []int64{2147483647, 0, -1, -2147483648}
	plane := make([]byte, 32)
	for i := range left {
		binary.LittleEndian.PutUint32(plane[i*8:], uint32(int32(left[i])))
		binary.LittleEndian.PutUint32(plane[i*8+4:], uint32(int32(right[i])))
	}
	buffer := [][]int64{make([]int64, 4), make([]int64, 4)}
	if err := deinterleaveS32(buffer, plane, 0, 4, 2, -2147483648, 2147483647, 32); err != nil {
		t.Fatalf("deinterleaveS32() error = %v", err)
	}
	for i := range left {
		if buffer[0][i] != left[i] || buffer[1][i] != right[i] {
			t.Fatalf("sample %d = (%d, %d), want (%d, %d)", i, buffer[0][i], buffer[1][i], left[i], right[i])
		}
	}
}

func TestDeinterleaveS32RejectsOutOfRange(t *testing.T) {
	t.Parallel()
	// bitsPerSample=8 restricts the valid signed range to [-128, 127]; 200
	// falls outside it.
	plane := make([]byte, 4)
	binary.LittleEndian.PutUint32(plane, uint32(int32(200)))
	buffer := [][]int64{make([]int64, 1)}
	if err := deinterleaveS32Scalar(buffer, plane, 0, 1, 1, -128, 127, 8); err == nil {
		t.Fatal("deinterleaveS32Scalar() error = nil, want out-of-range error")
	}
}
