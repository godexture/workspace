package timing

// Compare compares timestamps expressed in different time bases without
// rounding. It returns -1 when left is earlier, 0 when both timestamps are
// equal, and 1 when left is later. It returns ErrOverflow when exact
// comparison exceeds the supported 128-bit intermediate range.
func Compare(left int64, leftBase Base, right int64, rightBase Base) (int, error) {
	if !leftBase.Valid() || !rightBase.Valid() {
		return 0, ErrInvalidBase
	}

	if left == 0 || right == 0 {
		switch {
		case left == 0 && right == 0:
			return 0, nil
		case left == 0:
			if right < 0 {
				return 1, nil
			}
			return -1, nil
		case left < 0:
			return -1, nil
		default:
			return 1, nil
		}
	}

	leftNegative := left < 0
	rightNegative := right < 0
	if leftNegative != rightNegative {
		if leftNegative {
			return -1, nil
		}
		return 1, nil
	}

	leftFactors := [3]uint64{
		timestampMagnitude(left),
		uint64(leftBase.Numerator),
		uint64(rightBase.Denominator),
	}
	rightFactors := [3]uint64{
		timestampMagnitude(right),
		uint64(rightBase.Numerator),
		uint64(leftBase.Denominator),
	}
	reduceFactors(leftFactors[:], rightFactors[:])

	leftMagnitude, ok := multiplyFactors(leftFactors[:])
	if !ok {
		return 0, ErrOverflow
	}
	rightMagnitude, ok := multiplyFactors(rightFactors[:])
	if !ok {
		return 0, ErrOverflow
	}
	comparison := compare128(leftMagnitude, rightMagnitude)
	if leftNegative {
		return -comparison, nil
	}
	return comparison, nil
}

func timestampMagnitude(value int64) uint64 {
	if value < 0 {
		return uint64(-(value + 1)) + 1
	}
	return uint64(value)
}
