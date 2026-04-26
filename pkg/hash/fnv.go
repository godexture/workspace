package hash

func FNV(data []byte) uint64 {
	const (
		basis uint64 = 14695981039346656037
		prime uint64 = 1099511628211
	)

	var h uint64 = basis
	for _, c := range data {
		h ^= uint64(c)
		h *= prime
	}

	return h | (1 << 63)
}
