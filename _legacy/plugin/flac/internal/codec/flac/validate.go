package flac

import "fmt"

func ValidateSampleRange(samples []int64, bitsPerSample int) error {
	if bitsPerSample <= 0 || bitsPerSample > 32 {
		return fmt.Errorf("unsupported FLAC bit depth: %d", bitsPerSample)
	}
	min := -(int64(1) << uint(bitsPerSample-1))
	max := (int64(1) << uint(bitsPerSample-1)) - 1
	for _, sample := range samples {
		if sample < min || sample > max {
			return fmt.Errorf("FLAC sample %d outside %d-bit range", sample, bitsPerSample)
		}
	}
	return nil
}
