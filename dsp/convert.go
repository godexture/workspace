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
