//go:build !goexperiment.simd || !amd64

package decoder

func restoreLPC(samples, coefficients []int64, order, shift, bitsPerSample int, strict bool) error {
	if strict {
		min, max, err := sampleRangeBounds(bitsPerSample)
		if err != nil {
			return err
		}
		return restoreLPCScalar(samples, coefficients, order, shift, min, max, bitsPerSample)
	}
	restoreLPCScalarUnchecked(samples, coefficients, order, shift)
	return nil
}
