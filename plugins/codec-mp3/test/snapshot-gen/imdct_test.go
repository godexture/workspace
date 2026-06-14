//go:build cgo_test

package main

import (
	"math"
	"math/rand"
	"testing"

	"github.com/godexture/codec-mp3/internal/mp3"
)

func TestL3ImdctMatchesC(t *testing.T) {
	r := rand.New(rand.NewSource(12345))

	blockTypes := []int{0, 1, 2, 3}
	nLongBandsOptions := []int{0, 2, 4, 8, 32}

	for _, blockType := range blockTypes {
		for _, nLongBands := range nLongBandsOptions {
			// nLongBands must be <= 32
			if blockType == 2 {
				// mixed block: n_long_bands can be 2 or 0 depending on sample rate
				if nLongBands > 2 {
					continue
				}
			} else {
				if nLongBands > 0 {
					// for non-mixed blocks, n_long_bands is effectively 0 in minimp3.h logic,
					// or it is handled by the first if (n_long_bands). Let's test valid scenarios.
				}
			}

			t.Run(testing.TB(t).Name(), func(t *testing.T) {
				grbufC := make([]float32, 576)
				grbufGo := make([]float32, 576)
				for i := range grbufC {
					val := r.Float32()*2.0 - 1.0
					grbufC[i] = val
					grbufGo[i] = val
				}

				overlapC := make([]float32, 9*32)
				overlapGo := make([]float32, 9*32)
				for i := range overlapC {
					val := r.Float32()*2.0 - 1.0
					overlapC[i] = val
					overlapGo[i] = val
				}

				// Call C version
				C_imdct(grbufC, overlapC, blockType, nLongBands)

				// Call Go version
				mp3.L3Imdct(grbufGo, overlapGo, blockType, nLongBands)

				// Compare grbuf
				for i := range grbufC {
					diff := float64(grbufGo[i] - grbufC[i])
					if math.Abs(diff) > 1e-5 {
						t.Errorf("grbuf mismatch at index %d (blockType=%d, nLongBands=%d): Go=%f, C=%f (diff: %e)",
							i, blockType, nLongBands, grbufGo[i], grbufC[i], diff)
					}
				}

				// Compare overlap
				for i := range overlapC {
					diff := float64(overlapGo[i] - overlapC[i])
					if math.Abs(diff) > 1e-5 {
						t.Errorf("overlap mismatch at index %d (blockType=%d, nLongBands=%d): Go=%f, C=%f (diff: %e)",
							i, blockType, nLongBands, overlapGo[i], overlapC[i], diff)
					}
				}
			})
		}
	}
}
