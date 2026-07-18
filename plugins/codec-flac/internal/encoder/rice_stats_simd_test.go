//go:build goexperiment.simd && amd64

package encoder

import (
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/godexture/sdk/dsp"
)

func TestSumMaxUint64SIMD(t *testing.T) {
	requireAVX2(t)
	for _, values := range [][]uint64{
		nil,
		make([]uint64, 33),
		{0, 1, 1<<33 - 1, 7, 1<<32 - 1},
	} {
		assertSumMaxUint64Equal(t, values)
	}
	for _, length := range []int{0, 1, 2, 3, 4, 5, 8, 15, 16, 17, 31, 32, 33, 1000} {
		values := make([]uint64, length)
		for i := range values {
			values[i] = rand.Uint64() % (1 << 33)
		}
		assertSumMaxUint64Equal(t, values)
	}
}

func TestFoldResidualBatchSIMD(t *testing.T) {
	requireAVX2(t)
	boundaries := []int64{0, 1, -1, 2147483647, -2147483647}
	wantBoundaries := make([]uint64, len(boundaries))
	gotBoundaries := make([]uint64, len(boundaries))
	wantMax, wantOK := foldResidualBatchScalar(boundaries, wantBoundaries)
	gotMax, gotOK := foldResidualBatchSIMD(boundaries, gotBoundaries)
	if gotOK != wantOK || gotMax != wantMax || !slices.Equal(gotBoundaries, wantBoundaries) {
		t.Fatalf("boundaries: got max=%d ok=%v values=%v, want max=%d ok=%v values=%v", gotMax, gotOK, gotBoundaries, wantMax, wantOK, wantBoundaries)
	}
	for _, length := range []int{0, 1, 2, 3, 4, 15, 16, 17, 32, 33, 60, 4096, 4099} {
		residual := make([]int64, length)
		for i := range residual {
			residual[i] = int64(rand.Int32N(1<<31-1)) - int64(rand.Int32N(1<<31-1))
		}
		want := make([]uint64, length)
		got := make([]uint64, length)
		wantMax, wantOK := foldResidualBatchScalar(residual, want)
		gotMax, gotOK := foldResidualBatchSIMD(residual, got)
		if gotOK != wantOK || gotMax != wantMax || !slices.Equal(got, want) {
			t.Fatalf("length %d: got max=%d ok=%v, want max=%d ok=%v", length, gotMax, gotOK, wantMax, wantOK)
		}
	}

	for _, invalid := range []int64{-2147483648, 2147483648} {
		for _, index := range []int{0, 1, 2, 3, 16} {
			residual := make([]int64, 17)
			residual[index] = invalid
			folded := make([]uint64, len(residual))
			if _, ok := foldResidualBatchSIMD(residual, folded); ok {
				t.Fatalf("invalid residual %d at index %d accepted", invalid, index)
			}
		}
	}
}

func TestFoldSumMaxSIMD(t *testing.T) {
	requireAVX2(t)
	for _, length := range []int{0, 1, 2, 3, 4, 15, 16, 17, 32, 33, 60, 4096, 4099} {
		residual := make([]int64, length)
		for i := range residual {
			residual[i] = int64(rand.Int32N(1<<31-1)) - int64(rand.Int32N(1<<31-1))
		}
		wantSum, wantMax := foldSumMaxScalar(residual)
		gotSum, gotMax := foldSumMaxSIMD(residual)
		if gotSum != wantSum || gotMax != wantMax {
			t.Fatalf("length %d: got (%d, %d), want (%d, %d)", length, gotSum, gotMax, wantSum, wantMax)
		}
	}
}

func assertSumMaxUint64Equal(t *testing.T, values []uint64) {
	t.Helper()
	wantSum, wantMax := sumMaxUint64Scalar(values)
	gotSum, gotMax := sumMaxUint64SIMD(values)
	if gotSum != wantSum || gotMax != wantMax {
		t.Fatalf("values %v: got (%d, %d), want (%d, %d)", values, gotSum, gotMax, wantSum, wantMax)
	}
}

func requireAVX2(t *testing.T) {
	t.Helper()
	if !dsp.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
}
