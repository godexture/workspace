package decoder

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestInterleaveS8(t *testing.T) {
	t.Parallel()
	left := []int64{-128, -64, 0}
	right := []int64{127, 64, 0}
	plane := make([]byte, 6)
	interleaveS8(plane, [][]int64{left, right}, 3, 2)

	want := []byte{128, 127, 192, 64, 0, 0}
	if !bytes.Equal(plane, want) {
		t.Fatalf("interleaveS8() = % x, want % x", plane, want)
	}
}

func TestInterleaveS16(t *testing.T) {
	t.Parallel()
	left := []int64{-32768, 0, 32767}
	right := []int64{32767, 0, -32768}
	plane := make([]byte, 12)
	interleaveS16(plane, [][]int64{left, right}, 3, 2)

	want := make([]byte, 12)
	for i := range left {
		binary.LittleEndian.PutUint16(want[i*4:], uint16(int16(left[i])))
		binary.LittleEndian.PutUint16(want[i*4+2:], uint16(int16(right[i])))
	}
	if !bytes.Equal(plane, want) {
		t.Fatalf("interleaveS16() = % x, want % x", plane, want)
	}
}

func putS24(dst []byte, offset int, value int64) {
	u := uint32(value) & 0xffffff
	dst[offset] = byte(u)
	dst[offset+1] = byte(u >> 8)
	dst[offset+2] = byte(u >> 16)
}

func TestInterleaveS24(t *testing.T) {
	t.Parallel()
	left := []int64{-8388608, 0, 8388607}
	right := []int64{8388607, 0, -8388608}
	plane := make([]byte, 18)
	interleaveS24(plane, [][]int64{left, right}, 3, 2)

	want := make([]byte, 18)
	for i := range left {
		putS24(want, i*6, left[i])
		putS24(want, i*6+3, right[i])
	}
	if !bytes.Equal(plane, want) {
		t.Fatalf("interleaveS24() = % x, want % x", plane, want)
	}
}

func TestInterleaveS32(t *testing.T) {
	t.Parallel()
	// 4 samples so this also exercises the SIMD stereo path when available.
	left := []int64{-2147483648, -1, 0, 2147483647}
	right := []int64{2147483647, 0, -1, -2147483648}
	plane := make([]byte, 32)
	interleaveS32(plane, [][]int64{left, right}, 4, 2)

	want := make([]byte, 32)
	for i := range left {
		binary.LittleEndian.PutUint32(want[i*8:], uint32(int32(left[i])))
		binary.LittleEndian.PutUint32(want[i*8+4:], uint32(int32(right[i])))
	}
	if !bytes.Equal(plane, want) {
		t.Fatalf("interleaveS32() = % x, want % x", plane, want)
	}
}
