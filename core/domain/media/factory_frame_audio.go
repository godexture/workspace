package media

import (
	"github.com/godexture/core/domain/metadata"
	"github.com/godexture/sdk/pool"
)

type AudioFrameOption func(*AudioFrame)

func WithAudioPts(pts Pts) AudioFrameOption {
	return func(f *AudioFrame) {
		f.pts = pts
	}
}

func WithAudioBitsPerSample(bitsPerSample int) AudioFrameOption {
	return func(f *AudioFrame) {
		f.BitsPerSample = bitsPerSample
	}
}

func NewAudioFrame(format SampleFormat, layout ChannelLayout, sampleRate, samples int, opts ...AudioFrameOption) *AudioFrame {
	channels := layout.ChannelCount()
	bytesPerSample := format.BytesPerSample()
	totalBytes := channels * samples * bytesPerSample

	b := pool.Get(totalBytes)
	(*b) = (*b)[:totalBytes]

	frame := &AudioFrame{
		Format:        format,
		BitsPerSample: defaultBitsPerSample(format),
		Layout:        layout,
		SampleRate:    sampleRate,
		Samples:       samples,
		baseData:      b,
		meta:          metadata.NewBundle(),
		planes:        make([][]byte, channels),
	}
	frame.refCount.Store(1)

	if format.IsPlanar() {
		planeSize := samples * bytesPerSample
		for i := 0; i < channels; i++ {
			start := i * planeSize
			end := start + planeSize
			frame.planes[i] = (*b)[start:end]
		}
	} else {
		frame.planes[0] = *b
	}

	for _, opt := range opts {
		opt(frame)
	}

	frame.Init(func() {
		pool.Put(b)
		frame.meta.Clear()
	})

	return frame
}

func defaultBitsPerSample(format SampleFormat) int {
	switch format.Packed() {
	case SampleFormatU8:
		return 8
	case SampleFormatS16:
		return 16
	case SampleFormatS24:
		return 24
	case SampleFormatS32:
		return 32
	default:
		return 0
	}
}
