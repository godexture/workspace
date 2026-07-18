package encoder

func fixedResidualScalar(samples []int64, order int) []int64 {
	residual := getResidualBuffer(len(samples) - order)
	for i := order; i < len(samples); i++ {
		residual[i-order] = samples[i] - fixedPrediction(samples, i, order)
	}
	return residual
}

func lpcResidualScalar(samples []int64, order int, coefficients []int64, shift int) []int64 {
	coefficients = coefficients[:order:order]
	result := getResidualBuffer(len(samples) - order)
	for i := order; i < len(samples); i++ {
		var sum int64
		history := samples[i-order : i]
		for j, coefficient := range coefficients {
			sum += coefficient * history[order-1-j]
		}
		prediction := sum >> uint(shift)
		result[i-order] = samples[i] - prediction
	}
	return result
}
