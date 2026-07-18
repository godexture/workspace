//go:build goexperiment.simd && amd64

package encoder

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/godexture/sdk/dsp"
)

func TestAutocorrelateSIMD(t *testing.T) {
	if !dsp.HasAVX2FMA {
		t.Skip("AVX2 FMA unavailable")
	}
	for _, length := range []int{33, 47, 64, 257, 4097} {
		values := make([]float64, length)
		for i := range values {
			values[i] = float64(rand.Int32N(1<<25) - 1<<24)
		}
		for _, order := range []int{1, 8, 32} {
			if order >= length {
				continue
			}
			want := make([]float64, order+1)
			got := make([]float64, order+1)
			autocorrelateScalar(values, want)
			autocorrelateSIMD(values, got)
			for lag := range want {
				scale := math.Max(1, math.Abs(want[lag]))
				if math.Abs(got[lag]-want[lag])/scale > 1e-12 {
					t.Fatalf("length %d order %d lag %d: got %g, want %g", length, order, lag, got[lag], want[lag])
				}
			}
		}
	}
}
