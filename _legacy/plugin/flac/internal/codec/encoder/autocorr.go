package encoder

func autocorrelateScalar(values, auto []float64) {
	for lag := range auto {
		var sum float64
		for i := lag; i < len(values); i++ {
			sum += values[i] * values[i-lag]
		}
		auto[lag] = sum
	}
}
