package frame

import (
	"math/rand/v2"
	"testing"
)

func TestNextSyncPrefix(t *testing.T) {
	for length := 0; length <= 130; length++ {
		data := make([]byte, length)
		for i := range data {
			data[i] = byte(rand.Uint32())
		}
		for start := -1; start <= length+1; start++ {
			want := nextSyncPrefixReference(data, start)
			got := nextSyncPrefix(data, start)
			if got != want {
				t.Fatalf("length %d start %d: got %d, want %d", length, start, got, want)
			}
		}
	}
	for offset := 0; offset < 96; offset++ {
		data := make([]byte, 128)
		data[offset], data[offset+1] = 0xff, 0xfb
		if got := nextSyncPrefix(data, 0); got != offset {
			t.Fatalf("offset %d: got %d", offset, got)
		}
	}
}

func nextSyncPrefixReference(data []byte, start int) int {
	start = max(start, 0)
	for position := start; position+1 < len(data); position++ {
		if data[position] == 0xff && data[position+1]&0xfc == 0xf8 {
			return position
		}
	}
	return -1
}
