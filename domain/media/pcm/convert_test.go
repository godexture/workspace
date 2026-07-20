package pcm

import (
	"testing"

	"github.com/godexture/core/domain/media"
)

func TestPlanarAndPackedFormatsShareConversion(t *testing.T) {
	t.Parallel()
	values := []float32{-1, -0.25, 0.25, 1}
	for _, format := range []media.SampleFormat{media.SampleFormatS16, media.SampleFormatS16P} {
		t.Run(string(format), func(t *testing.T) {
			data := make([]byte, len(values)*format.BytesPerSample())
			if err := FromFloat32(data, values, format, 16); err != nil {
				t.Fatal(err)
			}
			decoded, err := ToFloat32(nil, data, format, 16)
			if err != nil {
				t.Fatal(err)
			}
			if len(decoded) != len(values) {
				t.Fatalf("sample count = %d, want %d", len(decoded), len(values))
			}
		})
	}
}
