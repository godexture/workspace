package encoder

import (
	"strconv"
	"testing"
)

func BenchmarkAutocorrelate(b *testing.B) {
	for _, order := range []int{8, 32} {
		b.Run("order"+strconv.Itoa(order), func(b *testing.B) {
			values := make([]float64, 4096)
			for i := range values {
				values[i] = float64((i*7919)%65536 - 32768)
			}
			auto := make([]float64, order+1)
			b.ReportAllocs()
			for b.Loop() {
				autocorrelate(values, auto)
			}
		})
	}
}
