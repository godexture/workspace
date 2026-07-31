package encoder

import "unsafe"

func loadScalarInt64At(base unsafe.Pointer, index int) int64 {
	return *(*int64)(unsafe.Add(base, uintptr(index)*8))
}

func storeScalarInt64At(base unsafe.Pointer, index int, value int64) {
	*(*int64)(unsafe.Add(base, uintptr(index)*8)) = value
}

func fixedPredictionAt(base unsafe.Pointer, index, order int) int64 {
	switch order {
	case 0:
		return 0
	case 1:
		return loadScalarInt64At(base, index-1)
	case 2:
		return 2*loadScalarInt64At(base, index-1) - loadScalarInt64At(base, index-2)
	case 3:
		return 3*loadScalarInt64At(base, index-1) -
			3*loadScalarInt64At(base, index-2) +
			loadScalarInt64At(base, index-3)
	case 4:
		return 4*loadScalarInt64At(base, index-1) -
			6*loadScalarInt64At(base, index-2) +
			4*loadScalarInt64At(base, index-3) -
			loadScalarInt64At(base, index-4)
	default:
		return 0
	}
}

func fixedResidualScalar(samples []int64, order int) []int64 {
	residual := getResidualBuffer(len(samples) - order)
	samplesBase := unsafe.Pointer(unsafe.SliceData(samples))
	residualBase := unsafe.Pointer(unsafe.SliceData(residual))
	// i >= order keeps every order-relative input and output index in range.
	for i := order; i < len(samples); i++ {
		value := loadScalarInt64At(samplesBase, i) - fixedPredictionAt(samplesBase, i, order)
		storeScalarInt64At(residualBase, i-order, value)
	}
	return residual
}

func lpcResidualScalar(samples []int64, order int, coefficients []int64, shift int) []int64 {
	coefficients = coefficients[:order:order]
	result := getResidualBuffer(len(samples) - order)
	samplesBase := unsafe.Pointer(unsafe.SliceData(samples))
	resultBase := unsafe.Pointer(unsafe.SliceData(result))
	// i >= order and j < order keep i-1-j and i-order in range.
	i := order
	for ; i+4 <= len(samples); i += 4 {
		var sum0, sum1, sum2, sum3 int64
		for j, coefficient := range coefficients {
			index := i - 1 - j
			sum0 += coefficient * loadScalarInt64At(samplesBase, index)
			sum1 += coefficient * loadScalarInt64At(samplesBase, index+1)
			sum2 += coefficient * loadScalarInt64At(samplesBase, index+2)
			sum3 += coefficient * loadScalarInt64At(samplesBase, index+3)
		}
		storeScalarInt64At(resultBase, i-order, loadScalarInt64At(samplesBase, i)-(sum0>>uint(shift)))
		storeScalarInt64At(resultBase, i-order+1, loadScalarInt64At(samplesBase, i+1)-(sum1>>uint(shift)))
		storeScalarInt64At(resultBase, i-order+2, loadScalarInt64At(samplesBase, i+2)-(sum2>>uint(shift)))
		storeScalarInt64At(resultBase, i-order+3, loadScalarInt64At(samplesBase, i+3)-(sum3>>uint(shift)))
	}
	for ; i < len(samples); i++ {
		var sum int64
		for j, coefficient := range coefficients {
			sum += coefficient * loadScalarInt64At(samplesBase, i-1-j)
		}
		prediction := sum >> uint(shift)
		storeScalarInt64At(resultBase, i-order, loadScalarInt64At(samplesBase, i)-prediction)
	}
	return result
}
