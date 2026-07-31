package pcm

import (
	"testing"

	"github.com/godexture/godec/core/domain/media"
)

func TestCreateAudioFramePreservesIntegerPCMGrid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		format        media.SampleFormat
		bitsPerSample int
	}{
		{name: "S16", format: media.SampleFormatS16, bitsPerSample: 16},
		{name: "12-bit in S16", format: media.SampleFormatS16, bitsPerSample: 12},
		{name: "24-bit in S32", format: media.SampleFormatS32, bitsPerSample: 24},
		{name: "S32", format: media.SampleFormatS32, bitsPerSample: 32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			scale := float32(uint64(1) << uint(tt.bitsPerSample-1))
			pcm := []float32{-1, -123 / scale, 0, 123 / scale, 1 - 1/scale}
			frame, err := CreateAudioFrame(pcm, media.AudioAttributes{
				SampleRate:    48000,
				Format:        tt.format,
				BitsPerSample: tt.bitsPerSample,
				ChannelLayout: media.LayoutMono1,
			})
			if err != nil {
				t.Fatal(err)
			}
			audioFrame := (*frame).(*media.AudioFrame)
			if audioFrame.BitsPerSample != tt.bitsPerSample {
				t.Fatalf("BitsPerSample = %d, want %d", audioFrame.BitsPerSample, tt.bitsPerSample)
			}
			got, err := ConvertToFloat32(nil, audioFrame)
			if err != nil {
				t.Fatal(err)
			}
			for i := range pcm {
				if got[i] != pcm[i] {
					t.Fatalf("sample %d = %v, want %v", i, got[i], pcm[i])
				}
			}
		})
	}
}

func TestCreateAudioFrameRejectsPrecisionWiderThanStorage(t *testing.T) {
	t.Parallel()
	_, err := CreateAudioFrame([]float32{0}, media.AudioAttributes{
		SampleRate:    48000,
		Format:        media.SampleFormatS16,
		BitsPerSample: 24,
		ChannelLayout: media.LayoutMono1,
	})
	if err == nil {
		t.Fatal("CreateAudioFrame() succeeded with 24-bit precision in S16 storage")
	}
}
