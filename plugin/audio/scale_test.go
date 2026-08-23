package audio

import (
	"math"
	"testing"

	"github.com/godexture/godec/media/sample"
)

// Widening between integer representations is exact, so a stream that passes
// through a wider one and back is the stream it started as. The float pivot is
// what has to preserve that, not the individual pairs.
func TestIntegerWideningRoundTripsExactly(t *testing.T) {
	values := []int16{-32768, -32767, -1, 0, 1, 32766, 32767}
	for _, value := range values {
		widened := make([]int32, 1)
		convert(widened, []int16{value})
		if widened[0] != int32(value)<<16 {
			t.Fatalf("s16 %d widened to %d, want %d", value, widened[0], int32(value)<<16)
		}
		restored := make([]int16, 1)
		convert(restored, widened)
		if restored[0] != value {
			t.Fatalf("s16 %d did not survive s32: %d", value, restored[0])
		}
		floats := make([]float64, 1)
		convert(floats, []int16{value})
		convert(restored, floats)
		if restored[0] != value {
			t.Fatalf("s16 %d did not survive f64: %d", value, restored[0])
		}
	}
}

func TestFullScaleMapsToTheNominalRange(t *testing.T) {
	floats := make([]float32, 1)
	convert(floats, []int16{-32768})
	if floats[0] != -1 {
		t.Fatalf("negative full scale = %v, want -1", floats[0])
	}
	convert(floats, []int16{32767})
	if floats[0] >= 1 || floats[0] < 0.9999 {
		t.Fatalf("positive full scale = %v, want just below 1", floats[0])
	}
	wide := make([]int32, 1)
	convert(wide, []float64{-1})
	if wide[0] != math.MinInt32 {
		t.Fatalf("negative full scale = %d", wide[0])
	}
}

// A signal past the nominal range saturates. Wrapping would turn a clipped
// peak into a full-scale sign flip, which is audible in a way clipping is not.
func TestOutOfRangeSignalsSaturate(t *testing.T) {
	narrowed := make([]int16, 4)
	convert(narrowed, []float32{2, -2, 1, -1})
	want := []int16{32767, -32768, 32767, -32768}
	for index := range want {
		if narrowed[index] != want[index] {
			t.Fatalf("saturated %d = %d, want %d", index, narrowed[index], want[index])
		}
	}
}

func TestConverterIdentityCoversEveryOrderedPair(t *testing.T) {
	codings := []sample.Coding{sample.S16, sample.S32, sample.F32, sample.F64}
	seen := make(map[string]struct{})
	for _, from := range codings {
		for _, to := range codings {
			identity := ConverterIdentity(from, to)
			if from == to {
				if !identity.IsZero() {
					t.Errorf("%s has a converter to itself", from)
				}
				continue
			}
			if identity.IsZero() {
				t.Fatalf("%s to %s has no converter", from, to)
			}
			seen[identity.String()] = struct{}{}
		}
	}
	if len(seen) != 12 {
		t.Fatalf("distinct converters = %d, want 12", len(seen))
	}
	if !ConverterIdentity(sample.U8, sample.S16).IsZero() {
		t.Error("a wire-only coding has a converter")
	}
}
