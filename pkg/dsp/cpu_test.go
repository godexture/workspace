package dsp

import "testing"

func TestCPUFeatures(t *testing.T) {
	if HasAVX2FMA && !HasAVX2 {
		t.Fatal("FMA enabled without AVX2")
	}
}
