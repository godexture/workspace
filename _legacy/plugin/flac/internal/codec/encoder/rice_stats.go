package encoder

func sumMaxUint64Scalar(values []uint64) (sum, maximum uint64) {
	for _, value := range values {
		sum += value
		if value > maximum {
			maximum = value
		}
	}
	return sum, maximum
}

func foldResidualBatchScalar(residual []int64, folded []uint64) (maximum uint64, ok bool) {
	for i, value := range residual {
		if !validFLACResidual(value) {
			return 0, false
		}
		folded[i] = foldResidual(value)
		if folded[i] > maximum {
			maximum = folded[i]
		}
	}
	return maximum, true
}

func foldSumMaxScalar(residual []int64) (sum, maximum uint64) {
	for _, value := range residual {
		folded := foldResidual(value)
		sum += folded
		if folded > maximum {
			maximum = folded
		}
	}
	return sum, maximum
}
