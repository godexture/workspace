package encoder

func computeMidSide(left, right, mid, side []int64) {
	for i := range left {
		mid[i] = (left[i] + right[i]) >> 1
		side[i] = left[i] - right[i]
	}
}
