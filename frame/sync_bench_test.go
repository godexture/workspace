package frame

import "testing"

func BenchmarkNextSyncPrefix(b *testing.B) {
	data := make([]byte, 256<<10)
	data[len(data)-2], data[len(data)-1] = 0xff, 0xf8
	b.SetBytes(int64(len(data)))
	b.Run("scalar", func(b *testing.B) {
		for b.Loop() {
			if nextSyncPrefixReference(data, 0) < 0 {
				b.Fatal("prefix not found")
			}
		}
	})
	b.Run("index-byte", func(b *testing.B) {
		for b.Loop() {
			if nextSyncPrefix(data, 0) < 0 {
				b.Fatal("prefix not found")
			}
		}
	})
}
