package dsp

import (
	"math"
	"testing"
)

func TestPCMFloat32RoundTrip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		kind PCMKind
		bits int
		tol  float32
	}{
		{"u8", PCMU8, 8, 1.0 / 127},
		{"s16", PCMS16, 16, 1.0 / 32767},
		{"s24", PCMS24, 20, 1.0 / 524287},
		{"s32", PCMS32, 32, 1.0 / 2147483647},
		{"f32", PCMF32, 0, 0},
		{"f64", PCMF64, 0, 0},
	}
	values := []float32{-1, -0.5, -0.125, 0, 0.125, 0.5, 1}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := make([]byte, len(values)*test.kind.BytesPerSample())
			if err := FromFloat32(data, values, test.kind, test.bits); err != nil {
				t.Fatal(err)
			}
			decoded, err := ToFloat32(nil, data, test.kind, test.bits)
			if err != nil {
				t.Fatal(err)
			}
			for i, want := range values {
				if diff := float32(math.Abs(float64(decoded[i] - want))); diff > test.tol {
					t.Fatalf("sample %d = %f, want %f (diff %f, tol %f)", i, decoded[i], want, diff, test.tol)
				}
			}
		})
	}
}

func TestPCMFromFloat32ClampsAndSanitizes(t *testing.T) {
	t.Parallel()
	data := make([]byte, 8)
	values := []float32{float32(math.Inf(-1)), float32(math.NaN()), float32(math.Inf(1)), 2}
	if err := FromFloat32(data, values, PCMS16, 16); err != nil {
		t.Fatal(err)
	}
	decoded, err := ToFloat32(nil, data, PCMS16, 16)
	if err != nil {
		t.Fatal(err)
	}
	if decoded[0] != -1 || decoded[1] != 0 || decoded[2] <= 0.999 || decoded[3] <= 0.999 {
		t.Fatalf("decoded clamped values = %v", decoded)
	}
}
