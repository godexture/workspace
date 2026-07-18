//go:build goexperiment.simd && amd64

package decoder

import (
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/godexture/sdk/dsp"
)

func TestRestoreLPCSIMD(t *testing.T) {
	if !dsp.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	for _, bitsPerSample := range []int{8, 16, 17, 24, 25, 32} {
		minimum, maximum, err := sampleRangeBounds(bitsPerSample)
		if err != nil {
			t.Fatal(err)
		}
		amplitude := min(int64(100), maximum)
		for order := 1; order <= 32; order++ {
			coefficients := make([]int64, order)
			for i := range coefficients {
				coefficients[i] = int64(i%5 - 2)
			}
			decoded := make([]int64, order+67)
			for i := range decoded {
				decoded[i] = rand.Int64N(2*amplitude+1) - amplitude
			}
			encoded := encodeLPCResiduals(decoded, coefficients, order, 7)
			want := slices.Clone(encoded)
			got := slices.Clone(encoded)
			wantErr := restoreLPCScalar(want, coefficients, order, 7, minimum, maximum, bitsPerSample)
			gotErr := restoreLPCSIMD(got, coefficients, order, 7, minimum, maximum, bitsPerSample)
			if errorText(gotErr) != errorText(wantErr) || !slices.Equal(got, want) || !slices.Equal(got, decoded) {
				t.Fatalf("bits=%d order=%d: got err=%v samples=%v, want err=%v samples=%v", bitsPerSample, order, gotErr, got, wantErr, want)
			}
		}
	}
}

func TestRestoreLPCSIMDErrorMatchesScalar(t *testing.T) {
	if !dsp.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	coefficients := []int64{1, 0, 0, 0}
	samples := []int64{32767, 0, 0, 32767, 32767}
	want := slices.Clone(samples)
	got := slices.Clone(samples)
	wantErr := restoreLPCScalar(want, coefficients, 4, 0, -32768, 32767, 16)
	gotErr := restoreLPCSIMD(got, coefficients, 4, 0, -32768, 32767, 16)
	if errorText(gotErr) != errorText(wantErr) {
		t.Fatalf("got error %v, want %v", gotErr, wantErr)
	}
}

func encodeLPCResiduals(decoded, coefficients []int64, order, shift int) []int64 {
	encoded := slices.Clone(decoded)
	for i := order; i < len(decoded); i++ {
		var prediction int64
		for j, coefficient := range coefficients {
			prediction += coefficient * decoded[i-1-j]
		}
		encoded[i] = decoded[i] - (prediction >> shift)
	}
	return encoded
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
