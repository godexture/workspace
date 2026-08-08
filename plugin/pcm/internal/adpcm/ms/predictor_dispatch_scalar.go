//go:build !goexperiment.simd || !amd64

package msadpcm

import "github.com/godexture/godec/plugin/pcm/internal/adpcm/param"

func findBestPredictor(chunkSamples []int16, samplesPerBlock, step, offset int, coefficients []param.Coefficient) int {
	return findBestPredictorScalar(chunkSamples, samplesPerBlock, step, offset, coefficients)
}
