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

func NewAudioFrame(format SampleFormat, layout ChannelLayout, sampleRate, samples int, opts ...AudioFrameOption) *AudioFrame {
	channels := layout.ChannelCount()
	bytesPerSample := format.BytesPerSample()
	totalBytes := channels * samples * bytesPerSample

	b := pool.Get(totalBytes)
	(*b) = (*b)[:totalBytes]

	frame := &AudioFrame{
		Format:     format,
		Layout:     layout,
		SampleRate: sampleRate,
		Samples:    samples,
		baseData:   b,
		Metadata:   metadata.NewBundle(),
		planes:     make([][]byte, channels),
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
		frame.Metadata.Clear()
	})

	return frame
}
