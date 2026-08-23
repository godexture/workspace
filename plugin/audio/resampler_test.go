package audio

import (
	"math"
	"testing"
)

// run drives the resampler the way the operator does, one chunk at a time,
// and returns everything it produced including the tail it owed at the end.
func run(kernel *resampler, in []float32, chunk int) []float32 {
	var result []float32
	for start := 0; start < len(in); start += chunk {
		end := min(start+chunk, len(in))
		window := [][]float32{in[start:end]}
		out := [][]float32{make([]float32, kernel.capacity(end-start))}
		written := kernel.produce(out, window)
		result = append(result, out[0][:written]...)
	}
	if pending := kernel.pending(); pending > 0 {
		out := [][]float32{make([]float32, pending)}
		result = append(result, out[0][:kernel.drain(out)]...)
	}
	return result
}

// Halving the rate leaves half the samples, and doubling it leaves twice as
// many. Rounding puts the count within one either way, which is what the tail
// exists to settle.
func TestResamplerProducesTheCountTheRatioImplies(t *testing.T) {
	for _, test := range []struct{ from, to, in, want int }{
		{from: 48_000, to: 24_000, in: 100, want: 50},
		{from: 48_000, to: 96_000, in: 100, want: 200},
		{from: 44_100, to: 48_000, in: 441, want: 480},
	} {
		got := len(run(newResampler(test.from, test.to, 1), make([]float32, test.in), 16))
		if got != test.want {
			t.Errorf("%d to %d over %d samples produced %d, want %d", test.from, test.to, test.in, got, test.want)
		}
	}
}

// Where the caller cut its frames is not something the output may depend on,
// which for a stage that reads across frame boundaries is the whole of the
// state it keeps.
func TestResamplerDoesNotDependOnWhereFramesWereCut(t *testing.T) {
	in := make([]float32, 300)
	for index := range in {
		in[index] = float32(math.Sin(float64(index) / 7))
	}
	whole := run(newResampler(48_000, 32_000, 1), in, len(in))
	for _, chunk := range []int{1, 7, 64, 299} {
		split := run(newResampler(48_000, 32_000, 1), in, chunk)
		if len(split) != len(whole) {
			t.Fatalf("chunks of %d produced %d samples, want %d", chunk, len(split), len(whole))
		}
		for index := range whole {
			if split[index] != whole[index] {
				t.Fatalf("chunks of %d: sample %d = %v, want %v", chunk, index, split[index], whole[index])
			}
		}
	}
}

// Interpolating between two samples has to land between them, and a ramp is
// where that is easiest to state: the output is the same ramp, read at the
// positions the new rate falls on.
func TestResamplerInterpolatesBetweenNeighbours(t *testing.T) {
	in := []float32{0, 1, 2, 3, 4, 5, 6, 7}
	got := run(newResampler(2, 1, 1), in, len(in))
	want := []float32{0, 2, 4, 6}
	if len(got) != len(want) {
		t.Fatalf("produced %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("sample %d = %v, want %v (got %v)", index, got[index], want[index], got)
		}
	}
}

// Every channel is read at the same positions, so a stream cannot come out of
// a rate change with its channels at different instants.
func TestResamplerKeepsChannelsTogether(t *testing.T) {
	kernel := newResampler(4, 3, 2)
	in := [][]float32{{0, 1, 2, 3, 4, 5, 6, 7}, {0, -1, -2, -3, -4, -5, -6, -7}}
	out := [][]float32{make([]float32, kernel.capacity(8)), make([]float32, kernel.capacity(8))}
	written := kernel.produce(out, in)
	if written == 0 {
		t.Fatal("no samples produced")
	}
	for index := range written {
		if out[0][index] != -out[1][index] {
			t.Fatalf("sample %d: channels %v and %v are not the same instant", index, out[0][index], out[1][index])
		}
	}
}
