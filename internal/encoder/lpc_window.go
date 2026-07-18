package encoder

func windowSamplesScalar(samples []int64, window, values []float64) {
	for i, sample := range samples {
		values[i] = float64(sample)
		if window != nil {
			values[i] *= window[i]
		}
	}
}
