//go:build !goexperiment.simd || !amd64

package msadpcm

import "github.com/godexture/godec/plugin/wave/params"

func findBestPredictor(chunkSamples []int16, samplesPerBlock, step, offset int, coefficients []params.Coefficient) int {
	return findBestPredictorScalar(chunkSamples, samplesPerBlock, step, offset, coefficients)
}
