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

func TestPCMMD5(t *testing.T) {
	t.Parallel()
	pcm := NewPCMMD5()
	pcm.Write([][]int64{{1, -2}, {3, -4}}, 16)
	want := md5.Sum([]byte{1, 0, 3, 0, 0xfe, 0xff, 0xfc, 0xff})
	if got := pcm.Sum(); got != want {
		t.Fatalf("Sum() = %x, want %x", got, want)
	}
}
