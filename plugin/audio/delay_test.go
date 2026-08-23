package audio

import "testing"

func newDelayKernel(length, channels int, feedback float32) filter {
	lines := make([][]float32, channels)
	for channel := range lines {
		lines[channel] = make([]float32, length)
	}
	return &delay{lines: lines, feedback: feedback, wet: 1, dry: 1}
}

func TestDelayKeepsItsLinesAcrossFrames(t *testing.T) {
	chunkInvariant(t, func() filter { return newDelayKernel(7, 2, 0.5) }, 2, 64)
}

// One sample in, and the same sample again a delay later. Without feedback
// that is the whole of the filter, so the positions of the two are the test.
func TestDelayRepeatsASampleOnce(t *testing.T) {
	planes := [][]float32{{1, 0, 0, 0, 0, 0, 0, 0}}
	newDelayKernel(4, 1, 0).Apply(planes)
	want := []float32{1, 0, 0, 0, 1, 0, 0, 0}
	for index := range want {
		if planes[0][index] != want[index] {
			t.Fatalf("sample %d = %v, want %v (got %v)", index, planes[0][index], want[index], planes[0])
		}
	}
}

// Feedback turns the single repeat into a series, each one the previous times
// the feedback, which is what makes it a decay rather than a loop.
func TestDelayFeedsRepeatsBackAtADecayingLevel(t *testing.T) {
	planes := [][]float32{make([]float32, 16)}
	planes[0][0] = 1
	newDelayKernel(4, 1, 0.5).Apply(planes)
	for position, want := range map[int]float32{4: 1, 8: 0.5, 12: 0.25} {
		if planes[0][position] != want {
			t.Fatalf("repeat at %d = %v, want %v (got %v)", position, planes[0][position], want, planes[0])
		}
	}
}
