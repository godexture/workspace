//go:build goexperiment.simd && amd64

package encoder

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"
)

func TestComputeMidSideSIMD(t *testing.T) {
	requireAVX2(t)
	for _, length := range []int{0, 1, 2, 3, 4, 5, 7, 8, 9, 4096, 4099} {
		left := make([]int64, length)
		right := make([]int64, length)
		for i := range left {
			left[i] = int64(rand.Int32())
			right[i] = int64(rand.Int32())
		}
		assertMidSideEqual(t, left, right)
	}

	assertMidSideEqual(t,
		[]int64{math.MinInt64, math.MaxInt64, math.MinInt32, math.MaxInt32, -1, 0, 1},
		[]int64{0, 1, math.MaxInt32, math.MinInt32, 1, -1, 0},
	)
}

func assertMidSideEqual(t *testing.T, left, right []int64) {
	t.Helper()
	wantMid := make([]int64, len(left))
	wantSide := make([]int64, len(left))
	gotMid := make([]int64, len(left))
	gotSide := make([]int64, len(left))
	computeMidSideScalar(left, right, wantMid, wantSide)
	computeMidSideSIMD(left, right, gotMid, gotSide)
	if !slices.Equal(gotMid, wantMid) || !slices.Equal(gotSide, wantSide) {
		t.Fatalf("got mid=%v side=%v, want mid=%v side=%v", gotMid, gotSide, wantMid, wantSide)
	}
}
