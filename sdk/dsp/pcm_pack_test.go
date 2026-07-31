package dsp

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestFromInt64S8(t *testing.T) {
	t.Parallel()
	left := []int64{-128, -64, 0}
	right := []int64{127, 64, 0}
	plane := make([]byte, 6)
	if err := FromInt64(plane, [][]int64{left, right}, PCMS8, 3, 2); err != nil {
		t.Fatalf("FromInt64(PCMS8) error = %v", err)
	}
	want := []byte{128, 127, 192, 64, 0, 0}
	if !bytes.Equal(plane, want) {
		t.Fatalf("FromInt64(PCMS8) = % x, want % x", plane, want)
	}
}

func TestFromInt64U8(t *testing.T) {
	t.Parallel()
	left := []int64{-128, -64, 0}
	right := []int64{127, 64, 0}
	plane := make([]byte, 6)
	if err := FromInt64(plane, [][]int64{left, right}, PCMU8, 3, 2); err != nil {
		t.Fatalf("FromInt64(PCMU8) error = %v", err)
	}
	want := []byte{0, 255, 64, 192, 128, 128}
	if !bytes.Equal(plane, want) {
		t.Fatalf("FromInt64(PCMU8) = % x, want % x", plane, want)
	}
}

func TestFromInt64S16(t *testing.T) {
	t.Parallel()
	left := []int64{-32768, 0, 32767}
	right := []int64{32767, 0, -32768}
	plane := make([]byte, 12)
	if err := FromInt64(plane, [][]int64{left, right}, PCMS16, 3, 2); err != nil {
		t.Fatalf("FromInt64(PCMS16) error = %v", err)
	}
	want := make([]byte, 12)
	for i := range left {
		binary.LittleEndian.PutUint16(want[i*4:], uint16(int16(left[i])))
		binary.LittleEndian.PutUint16(want[i*4+2:], uint16(int16(right[i])))
	}
	if !bytes.Equal(plane, want) {
		t.Fatalf("FromInt64(PCMS16) = % x, want % x", plane, want)
	}
}

func TestFromInt64S24(t *testing.T) {
	t.Parallel()
	left := []int64{-8388608, 0, 8388607}
	right := []int64{8388607, 0, -8388608}
	plane := make([]byte, 18)
	if err := FromInt64(plane, [][]int64{left, right}, PCMS24, 3, 2); err != nil {
		t.Fatalf("FromInt64(PCMS24) error = %v", err)
	}
	want := make([]byte, 18)
	for i := range left {
		putS24(want, i*6, left[i])
		putS24(want, i*6+3, right[i])
	}
	if !bytes.Equal(plane, want) {
		t.Fatalf("FromInt64(PCMS24) = % x, want % x", plane, want)
	}
}

func TestFromInt64S32(t *testing.T) {
	t.Parallel()
	// 4 samples so this also exercises the SIMD stereo path when available.
	left := []int64{-2147483648, -1, 0, 2147483647}
	right := []int64{2147483647, 0, -1, -2147483648}
	plane := make([]byte, 32)
	if err := FromInt64(plane, [][]int64{left, right}, PCMS32, 4, 2); err != nil {
		t.Fatalf("FromInt64(PCMS32) error = %v", err)
	}
	want := make([]byte, 32)
	for i := range left {
		binary.LittleEndian.PutUint32(want[i*8:], uint32(int32(left[i])))
		binary.LittleEndian.PutUint32(want[i*8+4:], uint32(int32(right[i])))
	}
	if !bytes.Equal(plane, want) {
		t.Fatalf("FromInt64(PCMS32) = % x, want % x", plane, want)
	}
}

func TestFromInt64RejectsUnsupportedKind(t *testing.T) {
	t.Parallel()
	if err := FromInt64(make([]byte, 4), [][]int64{{1}}, PCMF32, 1, 1); err == nil {
		t.Fatal("FromInt64(PCMF32) error = nil, want unsupported-kind error")
	}
}
