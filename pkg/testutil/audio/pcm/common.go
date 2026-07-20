package pcm

import (
	"fmt"

	"github.com/godexture/core/domain/media"
	mediapcm "github.com/godexture/core/domain/media/pcm"
)

// ConvertToFloat32 converts an AudioFrame's samples to float32, reusing dst when possible.
func ConvertToFloat32(dst []float32, af *media.AudioFrame) ([]float32, error) {
	plane := af.Planes()[0]
	channels := af.Layout.ChannelCount()
	samples := af.Samples
	totalSamples := samples * channels
	if af.Format.IsPlanar() {
		return nil, fmt.Errorf("ConvertToFloat32 does not interleave planar audio: %s", af.Format)
	}
	if len(plane) != totalSamples*af.Format.BytesPerSample() {
		return nil, fmt.Errorf("invalid packed audio plane size: got %d, want %d", len(plane), totalSamples*af.Format.BytesPerSample())
	}
	return mediapcm.ToFloat32(dst, plane, af.Format, af.BitsPerSample)
}
