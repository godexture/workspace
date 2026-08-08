package flac

import (
	"bytes"
	"crypto/md5"
	"testing"
)

func TestPackPCMMD5(t *testing.T) {
	t.Parallel()
	got := PackPCMMD5(nil, [][]int64{{-2, 1}, {3, -4}}, 12)
	want := []byte{0xfe, 0xff, 0x03, 0x00, 0x01, 0x00, 0xfc, 0xff}
	if !bytes.Equal(got, want) {
		t.Fatalf("PackPCMMD5() = %x, want %x", got, want)
	}
}

// TestPackPCMMD5Width1 guards against a regression where 8-bit audio
// (width == 1) fell through to the 4-byte-per-sample packer and wrote past
// the scratch buffer.
func TestPackPCMMD5Width1(t *testing.T) {
	t.Parallel()
	got := PackPCMMD5(nil, [][]int64{{-2, 1, 127}, {3, -4, -128}}, 8)
	want := []byte{0xfe, 0x03, 0x01, 0xfc, 0x7f, 0x80}
	if !bytes.Equal(got, want) {
		t.Fatalf("PackPCMMD5() = %x, want %x", got, want)
	}
}

// TestPackPCMMD5ReuseAcrossWidths exercises the scratch-buffer reuse path
// (PCMMD5.Write reuses m.scratch across calls) across every packed width in
// both directions (growing and shrinking), which is exactly the pattern the
// encoder/decoder hit per audio frame.
func TestPackPCMMD5ReuseAcrossWidths(t *testing.T) {
	t.Parallel()
	pcm := NewPCMMD5()
	calls := []struct {
		samples       [][]int64
		bitsPerSample int
	}{
		{[][]int64{{1, 2, 3, 4}}, 8},
		{[][]int64{{1, 2}, {3, 4}}, 16},
		{[][]int64{{1, 2, 3}}, 24},
		{[][]int64{{1}, {2}, {3}, {4}, {5}, {6}, {7}, {8}}, 32},
		{[][]int64{{9}}, 8},
	}
	var want []byte
	for _, call := range calls {
		want = append(want, PackPCMMD5(nil, call.samples, call.bitsPerSample)...)
		pcm.Write(call.samples, call.bitsPerSample)
	}
	if got, wantSum := pcm.Sum(), md5.Sum(want); got != wantSum {
		t.Fatalf("Sum() = %x, want %x", got, wantSum)
	}
}

func TestPCMMD5(t *testing.T) {
	t.Parallel()
	pcm := NewPCMMD5()
	pcm.Write([][]int64{{1, -2}, {3, -4}}, 16)
	want := md5.Sum([]byte{1, 0, 3, 0, 0xfe, 0xff, 0xfc, 0xff})
	if got := pcm.Sum(); got != want {
		t.Fatalf("Sum() = %x, want %x", got, want)
	}
}
