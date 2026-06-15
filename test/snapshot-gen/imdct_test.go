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
	numberOfLongBandsOptions := []int{0, 2, 4, 8, 32}

	for _, blockType := range blockTypes {
		for _, numberOfLongBands := range numberOfLongBandsOptions {
			// numberOfLongBands must be <= 32
			if blockType == 2 {
				// mixed block: numberOfLongBands can be 2 or 0 depending on sample rate
				if numberOfLongBands > 2 {
					continue
				}
			}

			t.Run(testing.TB(t).Name(), func(t *testing.T) {
				granuleBufferC := make([]float32, 576)
				granuleBufferGo := make([]float32, 576)
				for i := range granuleBufferC {
					val := r.Float32()*2.0 - 1.0
					granuleBufferC[i] = val
					granuleBufferGo[i] = val
				}

				overlapBufferC := make([]float32, 9*32)
				overlapBufferGo := make([]float32, 9*32)
				for i := range overlapBufferC {
					val := r.Float32()*2.0 - 1.0
					overlapBufferC[i] = val
					overlapBufferGo[i] = val
				}

				// Call C version
				C_imdct(granuleBufferC, overlapBufferC, blockType, numberOfLongBands)

				// Call Go version
				mp3.L3Imdct(granuleBufferGo, overlapBufferGo, blockType, numberOfLongBands)

				// Compare granuleBuffer
				for i := range granuleBufferC {
					diff := float64(granuleBufferGo[i] - granuleBufferC[i])
					if math.Abs(diff) > 1e-5 {
						t.Errorf("granuleBuffer mismatch at index %d (blockType=%d, numberOfLongBands=%d): Go=%f, C=%f (diff: %e)",
							i, blockType, numberOfLongBands, granuleBufferGo[i], granuleBufferC[i], diff)
					}
				}

				// Compare overlapBuffer
				for i := range overlapBufferC {
					diff := float64(overlapBufferGo[i] - overlapBufferC[i])
					if math.Abs(diff) > 1e-5 {
						t.Errorf("overlapBuffer mismatch at index %d (blockType=%d, numberOfLongBands=%d): Go=%f, C=%f (diff: %e)",
							i, blockType, numberOfLongBands, overlapBufferGo[i], overlapBufferC[i], diff)
					}
				}
			})
		}
	}
}
