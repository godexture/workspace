package fft

// transformRadix2Scalar runs an in-place radix-2 decimation-in-time FFT
// (or, when invert is true, its inverse) over data, using precomputed
// twiddle factors and a precomputed bit-reversal permutation.
//
// This is the only kernel today. It is kept in its own file, separate from
// Plan's precomputation and validation logic, so that a future SIMD kernel
// (mirroring the dsp package's HasAVX2 dispatch pattern) can be added
// alongside it without touching Plan itself.
func transformRadix2Scalar(twiddles []complex64, bitrev []int32, data []complex64, invert bool) {
	n := len(data)
	for i, r := range bitrev {
		ri := int(r)
		if i < ri {
			data[i], data[ri] = data[ri], data[i]
		}
	}

	for length := 2; length <= n; length <<= 1 {
		half := length / 2
		stride := n / length
		for i := 0; i < n; i += length {
			for j := 0; j < half; j++ {
				w := twiddles[j*stride]
				if invert {
					w = complex(real(w), -imag(w))
				}
				u := data[i+j]
				v := data[i+j+half] * w
				data[i+j] = u + v
				data[i+j+half] = u - v
			}
		}
	}

	if invert {
		scale := complex(1/float32(n), float32(0))
		for i := range data {
			data[i] *= scale
		}
	}
}
