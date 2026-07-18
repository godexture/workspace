//go:build goexperiment.simd && amd64

package encoder

import (
	"strconv"
	"testing"
	"unsafe"

	"simd/archsimd"
)

// Experimental variant A: unsafe pointer loads + bounds-check-free coefficient
// access + 2 accumulators, 4 outputs per iteration.
func lpcResidualSIMDExpA(samples []int64, order int, coefficients []int64, shift int) []int64 {
	coefficients = coefficients[:order:order]
	result := getResidualBuffer(len(samples) - order)
	var coefficientVectors [32]archsimd.Int32x8
	for i, coefficient := range coefficients {
		coefficientVectors[i] = archsimd.BroadcastInt64x4(coefficient).AsInt32x8()
	}
	vectors := coefficientVectors[:order:order]
	shiftAmount := uint64(shift)
	topBit, bias := int64x4ShiftRightConstants(shiftAmount)

	base := unsafe.Pointer(unsafe.SliceData(samples))
	i := order
	for ; i+4 <= len(samples); i += 4 {
		var sum0, sum1 archsimd.Int64x4
		p := unsafe.Add(base, uintptr(i-1)*8)
		j := 0
		for ; j+2 <= order; j += 2 {
			v0 := archsimd.LoadInt64x4((*[4]int64)(unsafe.Add(p, -uintptr(j)*8)))
			v1 := archsimd.LoadInt64x4((*[4]int64)(unsafe.Add(p, -uintptr(j+1)*8)))
			sum0 = sum0.Add(v0.AsInt32x8().MulEvenWiden(vectors[j]))
			sum1 = sum1.Add(v1.AsInt32x8().MulEvenWiden(vectors[j+1]))
		}
		if j < order {
			v := archsimd.LoadInt64x4((*[4]int64)(unsafe.Add(p, -uintptr(j)*8)))
			sum0 = sum0.Add(v.AsInt32x8().MulEvenWiden(vectors[j]))
		}
		prediction := shiftRightInt64x4(sum0.Add(sum1), shiftAmount, topBit, bias)
		archsimd.LoadInt64x4((*[4]int64)(unsafe.Add(base, uintptr(i)*8))).Sub(prediction).StoreSlice(result[i-order:])
	}
	for ; i < len(samples); i++ {
		var sum int64
		for j, coefficient := range coefficients {
			sum += coefficient * samples[i-1-j]
		}
		result[i-order] = samples[i] - (sum >> uint(shift))
	}
	return result
}

// Experimental variant B: like A but 8 outputs per iteration (4 accumulators).
func lpcResidualSIMDExpB(samples []int64, order int, coefficients []int64, shift int) []int64 {
	coefficients = coefficients[:order:order]
	result := getResidualBuffer(len(samples) - order)
	var coefficientVectors [32]archsimd.Int32x8
	for i, coefficient := range coefficients {
		coefficientVectors[i] = archsimd.BroadcastInt64x4(coefficient).AsInt32x8()
	}
	vectors := coefficientVectors[:order:order]
	shiftAmount := uint64(shift)
	topBit, bias := int64x4ShiftRightConstants(shiftAmount)

	base := unsafe.Pointer(unsafe.SliceData(samples))
	i := order
	for ; i+8 <= len(samples); i += 8 {
		var sumA0, sumA1, sumB0, sumB1 archsimd.Int64x4
		p := unsafe.Add(base, uintptr(i-1)*8)
		q := unsafe.Add(base, uintptr(i+3)*8)
		j := 0
		for ; j+2 <= order; j += 2 {
			c0, c1 := vectors[j], vectors[j+1]
			sumA0 = sumA0.Add(archsimd.LoadInt64x4((*[4]int64)(unsafe.Add(p, -uintptr(j)*8))).AsInt32x8().MulEvenWiden(c0))
			sumA1 = sumA1.Add(archsimd.LoadInt64x4((*[4]int64)(unsafe.Add(p, -uintptr(j+1)*8))).AsInt32x8().MulEvenWiden(c1))
			sumB0 = sumB0.Add(archsimd.LoadInt64x4((*[4]int64)(unsafe.Add(q, -uintptr(j)*8))).AsInt32x8().MulEvenWiden(c0))
			sumB1 = sumB1.Add(archsimd.LoadInt64x4((*[4]int64)(unsafe.Add(q, -uintptr(j+1)*8))).AsInt32x8().MulEvenWiden(c1))
		}
		if j < order {
			c := vectors[j]
			sumA0 = sumA0.Add(archsimd.LoadInt64x4((*[4]int64)(unsafe.Add(p, -uintptr(j)*8))).AsInt32x8().MulEvenWiden(c))
			sumB0 = sumB0.Add(archsimd.LoadInt64x4((*[4]int64)(unsafe.Add(q, -uintptr(j)*8))).AsInt32x8().MulEvenWiden(c))
		}
		predictionA := shiftRightInt64x4(sumA0.Add(sumA1), shiftAmount, topBit, bias)
		predictionB := shiftRightInt64x4(sumB0.Add(sumB1), shiftAmount, topBit, bias)
		archsimd.LoadInt64x4((*[4]int64)(unsafe.Add(base, uintptr(i)*8))).Sub(predictionA).StoreSlice(result[i-order:])
		archsimd.LoadInt64x4((*[4]int64)(unsafe.Add(base, uintptr(i+4)*8))).Sub(predictionB).StoreSlice(result[i-order+4:])
	}
	for ; i < len(samples); i++ {
		var sum int64
		for j, coefficient := range coefficients {
			sum += coefficient * samples[i-1-j]
		}
		result[i-order] = samples[i] - (sum >> uint(shift))
	}
	return result
}

// Experimental variant C: safe slice-based loads (no unsafe), 2 accumulators.
func lpcResidualSIMDExpC(samples []int64, order int, coefficients []int64, shift int) []int64 {
	coefficients = coefficients[:order:order]
	result := getResidualBuffer(len(samples) - order)
	var coefficientVectors [32]archsimd.Int32x8
	for i, coefficient := range coefficients {
		coefficientVectors[i] = archsimd.BroadcastInt64x4(coefficient).AsInt32x8()
	}
	vectors := coefficientVectors[:order:order]
	shiftAmount := uint64(shift)
	topBit, bias := int64x4ShiftRightConstants(shiftAmount)

	i := order
	for ; i+4 <= len(samples); i += 4 {
		var sum0, sum1 archsimd.Int64x4
		j := 0
		for ; j+2 <= order; j += 2 {
			v0 := archsimd.LoadInt64x4Slice(samples[i-1-j:])
			v1 := archsimd.LoadInt64x4Slice(samples[i-2-j:])
			sum0 = sum0.Add(v0.AsInt32x8().MulEvenWiden(vectors[j]))
			sum1 = sum1.Add(v1.AsInt32x8().MulEvenWiden(vectors[j+1]))
		}
		if j < order {
			v := archsimd.LoadInt64x4Slice(samples[i-1-j:])
			sum0 = sum0.Add(v.AsInt32x8().MulEvenWiden(vectors[j]))
		}
		prediction := shiftRightInt64x4(sum0.Add(sum1), shiftAmount, topBit, bias)
		archsimd.LoadInt64x4Slice(samples[i:]).Sub(prediction).StoreSlice(result[i-order:])
	}
	for ; i < len(samples); i++ {
		var sum int64
		for j, coefficient := range coefficients {
			sum += coefficient * samples[i-1-j]
		}
		result[i-order] = samples[i] - (sum >> uint(shift))
	}
	return result
}

// Experimental variant D: safe slice-based loads (no unsafe), 8 outputs / iteration, 4 accumulators.
func lpcResidualSIMDExpD(samples []int64, order int, coefficients []int64, shift int) []int64 {
	coefficients = coefficients[:order:order]
	result := getResidualBuffer(len(samples) - order)
	var coefficientVectors [32]archsimd.Int32x8
	for i, coefficient := range coefficients {
		coefficientVectors[i] = archsimd.BroadcastInt64x4(coefficient).AsInt32x8()
	}
	vectors := coefficientVectors[:order:order]
	shiftAmount := uint64(shift)
	topBit, bias := int64x4ShiftRightConstants(shiftAmount)

	i := order
	for ; i+8 <= len(samples); i += 8 {
		var sumA0, sumA1, sumB0, sumB1 archsimd.Int64x4
		j := 0
		for ; j+2 <= order; j += 2 {
			c0, c1 := vectors[j], vectors[j+1]
			sumA0 = sumA0.Add(archsimd.LoadInt64x4Slice(samples[i-1-j:]).AsInt32x8().MulEvenWiden(c0))
			sumA1 = sumA1.Add(archsimd.LoadInt64x4Slice(samples[i-2-j:]).AsInt32x8().MulEvenWiden(c1))
			sumB0 = sumB0.Add(archsimd.LoadInt64x4Slice(samples[i+3-j:]).AsInt32x8().MulEvenWiden(c0))
			sumB1 = sumB1.Add(archsimd.LoadInt64x4Slice(samples[i+2-j:]).AsInt32x8().MulEvenWiden(c1))
		}
		if j < order {
			c := vectors[j]
			sumA0 = sumA0.Add(archsimd.LoadInt64x4Slice(samples[i-1-j:]).AsInt32x8().MulEvenWiden(c))
			sumB0 = sumB0.Add(archsimd.LoadInt64x4Slice(samples[i+3-j:]).AsInt32x8().MulEvenWiden(c))
		}
		predictionA := shiftRightInt64x4(sumA0.Add(sumA1), shiftAmount, topBit, bias)
		predictionB := shiftRightInt64x4(sumB0.Add(sumB1), shiftAmount, topBit, bias)
		archsimd.LoadInt64x4Slice(samples[i:]).Sub(predictionA).StoreSlice(result[i-order:])
		archsimd.LoadInt64x4Slice(samples[i+4:]).Sub(predictionB).StoreSlice(result[i-order+4:])
	}
	for ; i < len(samples); i++ {
		var sum int64
		for j, coefficient := range coefficients {
			sum += coefficient * samples[i-1-j]
		}
		result[i-order] = samples[i] - (sum >> uint(shift))
	}
	return result
}

func TestLPCResidualExpMatchesScalar(t *testing.T) {
	samples := benchmarkBlock(4099)[0]
	for _, order := range []int{1, 2, 3, 5, 8, 12, 16, 31, 32} {
		coefficients := make([]int64, order)
		for i := range coefficients {
			coefficients[i] = int64((i*2654435761)%4001 - 2000)
		}
		want := lpcResidualScalar(samples, order, coefficients, 7)
		for name, fn := range map[string]func([]int64, int, []int64, int) []int64{
			"A": lpcResidualSIMDExpA, "B": lpcResidualSIMDExpB,
			"C": lpcResidualSIMDExpC, "D": lpcResidualSIMDExpD,
		} {
			got := fn(samples, order, coefficients, 7)
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("%s order %d: mismatch at %d: got %d want %d", name, order, i, got[i], want[i])
				}
			}
			releaseResidualBuffer(got)
		}
		releaseResidualBuffer(want)
	}
}

func BenchmarkLPCResidualExp(b *testing.B) {
	samples := benchmarkBlock(4096)[0]
	for _, order := range []int{8, 16, 32} {
		coefficients := make([]int64, order)
		for i := range coefficients {
			coefficients[i] = int64(i%7 - 3)
		}
		b.Run("order"+strconv.Itoa(order), func(b *testing.B) {
			b.Run("current", func(b *testing.B) {
				for b.Loop() {
					releaseResidualBuffer(lpcResidualSIMD(samples, order, coefficients, 7))
				}
			})
			b.Run("expA", func(b *testing.B) {
				for b.Loop() {
					releaseResidualBuffer(lpcResidualSIMDExpA(samples, order, coefficients, 7))
				}
			})
			b.Run("expB", func(b *testing.B) {
				for b.Loop() {
					releaseResidualBuffer(lpcResidualSIMDExpB(samples, order, coefficients, 7))
				}
			})
			b.Run("expC", func(b *testing.B) {
				for b.Loop() {
					releaseResidualBuffer(lpcResidualSIMDExpC(samples, order, coefficients, 7))
				}
			})
			b.Run("expD", func(b *testing.B) {
				for b.Loop() {
					releaseResidualBuffer(lpcResidualSIMDExpD(samples, order, coefficients, 7))
				}
			})
			b.Run("scalar", func(b *testing.B) {
				for b.Loop() {
					releaseResidualBuffer(lpcResidualScalar(samples, order, coefficients, 7))
				}
			})
		})
	}
}
