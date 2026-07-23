package dsp

import (
	"math"
	"testing"
)

func TestClampL1ScalesDownExcessiveRows(t *testing.T) {
	t.Parallel()
	got := ClampL1([][]float32{{1, 1}, {0.25, -0.25}})
	want := [][]float32{{0.5, 0.5}, {0.25, -0.25}}
	for i := range want {
		for j := range want[i] {
			if diff := float64(got[i][j] - want[i][j]); math.Abs(diff) > 1e-6 {
				t.Fatalf("row %d = %v, want %v", i, got[i], want[i])
			}
		}
	}
}

func TestClampL1NeverAmplifies(t *testing.T) {
	t.Parallel()
	rows := [][]float64{{0.1, -0.1, 0.05}}
	got := ClampL1(rows)
	for j := range rows[0] {
		if got[0][j] != rows[0][j] {
			t.Fatalf("row unexpectedly modified: got %v, want %v", got[0], rows[0])
		}
	}
}

func TestClampL1HandlesEmptyRow(t *testing.T) {
	t.Parallel()
	got := ClampL1([][]float32{{}})
	if len(got) != 1 || len(got[0]) != 0 {
		t.Fatalf("got %v, want a single empty row", got)
	}
}
