package audio

import "testing"

func newDCOffsetKernel(channels int) filter {
	return &dcOffset{pole: 0.995, lastX: make([]float32, channels), lastY: make([]float32, channels)}
}

func TestDCOffsetKeepsItsStateAcrossFrames(t *testing.T) {
	chunkInvariant(t, func() filter { return newDCOffsetKernel(2) }, 2, 64)
}

// A constant is exactly what this filter exists to remove, so a steady input
// has to decay toward silence rather than settle anywhere else.
func TestDCOffsetDecaysAConstantTowardSilence(t *testing.T) {
	planes := [][]float32{make([]float32, 512)}
	for index := range planes[0] {
		planes[0][index] = 1
	}
	newDCOffsetKernel(1).Apply(planes)
	if planes[0][0] != 1 {
		t.Fatalf("first sample = %v, want the input unchanged before any history exists", planes[0][0])
	}
	for index := 1; index < len(planes[0]); index++ {
		if planes[0][index] >= planes[0][index-1] {
			t.Fatalf("sample %d = %v did not decay below %v", index, planes[0][index], planes[0][index-1])
		}
	}
	if planes[0][len(planes[0])-1] > 0.1 {
		t.Fatalf("constant left %v behind after 512 samples", planes[0][len(planes[0])-1])
	}
}
