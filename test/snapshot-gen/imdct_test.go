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
	longBandCountOptions := []int{0, 2, 4, 8, 32}

	for _, blockType := range blockTypes {
		for _, longBandCount := range longBandCountOptions {
			// longBandCount must be <= 32
			if blockType == 2 {
				// mixed block: longBandCount can be 2 or 0 depending on sample rate
				if longBandCount > 2 {
					continue
				}
			}

			t.Run(testing.TB(t).Name(), func(t *testing.T) {
				granuleC := make([]float32, 576)
				granuleGo := make([]float32, 576)
				for i := range granuleC {
					val := r.Float32()*2.0 - 1.0
					granuleC[i] = val
					granuleGo[i] = val
				}

				overlapBufferC := make([]float32, 9*32)
				overlapBufferGo := make([]float32, 9*32)
				for i := range overlapBufferC {
					val := r.Float32()*2.0 - 1.0
					overlapBufferC[i] = val
					overlapBufferGo[i] = val
				}

				// Call C version
				C_imdct(granuleC, overlapBufferC, blockType, longBandCount)

				// Call Go version
				mp3.L3Imdct(granuleGo, overlapBufferGo, blockType, longBandCount)

				// Compare granule
				for i := range granuleC {
					diff := float64(granuleGo[i] - granuleC[i])
					if math.Abs(diff) > 1e-5 {
						t.Errorf("granule mismatch at index %d (blockType=%d, longBandCount=%d): Go=%f, C=%f (diff: %e)",
							i, blockType, longBandCount, granuleGo[i], granuleC[i], diff)
					}
				}

				// Compare overlapBuffer
				for i := range overlapBufferC {
					diff := float64(overlapBufferGo[i] - overlapBufferC[i])
					if math.Abs(diff) > 1e-5 {
						t.Errorf("overlapBuffer mismatch at index %d (blockType=%d, longBandCount=%d): Go=%f, C=%f (diff: %e)",
							i, blockType, longBandCount, overlapBufferGo[i], overlapBufferC[i], diff)
					}
				}
			})
		}
	}
}
