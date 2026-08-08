//go:build goexperiment.simd && amd64

package dsp

import (
	"encoding/binary"
	"math/rand/v2"
	"slices"
	"testing"
)

func TestUnpackS32StereoSIMD(t *testing.T) {
	if !HasAVX2() {
		t.Skip("AVX2 unavailable")
	}
	for _, samples := range []int{0, 1, 2, 3, 4, 5, 7, 8, 9, 4096, 4099} {
		values := make([]int32, samples*2)
		for i := 0; i < samples; i++ {
			values[i*2] = int32(1000 + i)
			values[i*2+1] = int32(2000 + i)
		}
		if samples > 9 {
			for i := range values {
				values[i] = rand.Int32()
			}
		}
		assertUnpackS32Equal(t, values, 3, -1<<31, 1<<31-1, 32, false)
	}
}

func TestUnpackS32StereoSIMDErrorsMatchScalar(t *testing.T) {
	if !HasAVX2() {
		t.Skip("AVX2 unavailable")
	}
	for _, invalid := range []int32{-1 << 20, 1 << 20} {
		for rawIndex := 0; rawIndex < 10; rawIndex++ {
			values := make([]int32, 10)
			values[rawIndex] = invalid
			assertUnpackS32Equal(t, values, 2, -32768, 32767, 16, false)
		}
	}
}

func TestUnpackS32StereoSIMDMisaligned(t *testing.T) {
	if !HasAVX2() {
		t.Skip("AVX2 unavailable")
	}
	values := []int32{1000, 2000, 1001, 2001, 1002, 2002, 1003, 2003}
	assertUnpackS32Equal(t, values, 1, -1<<31, 1<<31-1, 32, true)
}

func TestUnpackS32DispatchesMonoToScalar(t *testing.T) {
	if !HasAVX2() {
		t.Skip("AVX2 unavailable")
	}
	plane := packInt32Samples([]int32{1, 2, 3, 4}, false)
	want := [][]int64{make([]int64, 4)}
	got := [][]int64{make([]int64, 4)}
	if err := unpackS32Scalar(want, plane, 0, 4, 1, -1<<31, 1<<31-1, 32); err != nil {
		t.Fatal(err)
	}
	if err := unpackS32(got, plane, 0, 4, 1, -1<<31, 1<<31-1, 32); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got[0], want[0]) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func assertUnpackS32Equal(t *testing.T, values []int32, writeStart int, minimum, maximum int64, bitsPerSample int, misaligned bool) {
	t.Helper()
	plane := packInt32Samples(values, misaligned)
	samples := len(values) / 2
	want := [][]int64{make([]int64, writeStart+samples), make([]int64, writeStart+samples)}
	got := [][]int64{make([]int64, writeStart+samples), make([]int64, writeStart+samples)}
	for ch := range want {
		for i := 0; i < writeStart; i++ {
			want[ch][i] = -99
			got[ch][i] = -99
		}
	}
	wantErr := unpackS32Scalar(want, plane, writeStart, samples, 2, minimum, maximum, bitsPerSample)
	gotErr := unpackS32StereoSIMD(got, plane, writeStart, samples, minimum, maximum, bitsPerSample)
	if errorText(gotErr) != errorText(wantErr) || !slices.Equal(got[0], want[0]) || !slices.Equal(got[1], want[1]) {
		t.Fatalf("got error=%v buffer=%v, want error=%v buffer=%v", gotErr, got, wantErr, want)
	}
}

func packInt32Samples(values []int32, misaligned bool) []byte {
	offset := 0
	if misaligned {
		offset = 1
	}
	storage := make([]byte, offset+len(values)*4)
	plane := storage[offset:]
	for i, value := range values {
		binary.LittleEndian.PutUint32(plane[i*4:], uint32(value))
	}
	return plane
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
