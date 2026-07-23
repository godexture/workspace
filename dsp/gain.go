package dsp

import "math"

// ClampL1 scales each row down, independently, if the sum of its absolute
// values (its L1 norm) exceeds 1: for a row of coefficients applied
// linearly to inputs within [-1, 1] (an impulse response, a mixing
// matrix's weights, ...), that bounds the maximum possible output
// magnitude to at most 1, avoiding worst-case clipping. Rows already
// within bound are returned unmodified — this only ever attenuates, never
// amplifies.
func ClampL1[T ~float32 | ~float64](rows [][]T) [][]T {
	result := make([][]T, len(rows))
	for i, row := range rows {
		var l1 float64
		for _, v := range row {
			l1 += math.Abs(float64(v))
		}
		if l1 <= 1 {
			result[i] = row
			continue
		}
		scale := T(1 / l1)
		scaled := make([]T, len(row))
		for j, v := range row {
			scaled[j] = v * scale
		}
		result[i] = scaled
	}
	return result
}
