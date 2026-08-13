// Package dsp provides sample conversion, gain, and packing helpers.
//
// Its only consumer is an unreached ADPCM predictor; the codecs that used the
// rest are still in _legacy pending the M8 family migration, which also
// decides whether this stays a public package. Treat the API as unstable
// until then.
package dsp

func convertF32ToS16Scalar(destination []int16, source []float32) {
	length := min(len(destination), len(source))
	for i, sample := range source[:length] {
		if sample > 1 {
			sample = 1
		} else if sample < -1 {
			sample = -1
		}
		if sample < 0 {
			destination[i] = int16(sample * 32768)
		} else {
			destination[i] = int16(sample * 32767)
		}
	}
}
