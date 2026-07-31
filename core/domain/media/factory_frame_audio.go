package media

import (
	"github.com/godexture/godec/sdk/pool"
)

// audioFramePool reuses *AudioFrame objects the way packetPool reuses
// *Packet (see factory_packet.go): freeFunc is bound once per pooled object,
// not per NewAudioFrame call, so repeated frame construction doesn't pay for
// a fresh struct, planes slice, and closure on every call.
//
// New is assigned in init rather than the var declaration: free's body
// refers to audioFramePool, so inlining the closure into the initializer
// would make audioFramePool's own initializer depend on free, which depends
// back on audioFramePool -- an initialization cycle the compiler rejects.
var audioFramePool pool.Typed[*AudioFrame]

func init() {
	audioFramePool.Init(func() *AudioFrame {
		frame := &AudioFrame{}
		frame.freeFunc = frame.free
		return frame
	})
}

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

	frame := audioFramePool.Get()
	frame.baseData = b
	frame.Format = format
	frame.BitsPerSample = format.BitsPerSample()
	frame.Layout = layout
	frame.SampleRate = sampleRate
	frame.Samples = samples
	frame.pts = 0
	if cap(frame.planes) < channels {
		frame.planes = make([][]byte, channels)
	} else {
		frame.planes = frame.planes[:channels]
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

	return frame
}

// free returns the frame's byte payload to the buffer pool and the frame
// itself to audioFramePool. It is bound to freeFunc once per pooled object
// (see audioFramePool.New), so it always reads this call's current field
// values rather than values captured at bind time.
func (f *AudioFrame) free() {
	pool.Put(f.baseData)
	f.baseData = nil
	f.Format = SampleFormatUnknown
	f.BitsPerSample = 0
	f.Layout = ChannelLayout{}
	f.SampleRate = 0
	f.Samples = 0
	f.pts = 0
	for i := range f.planes {
		f.planes[i] = nil
	}
	f.planes = f.planes[:0]
	audioFramePool.Put(f)
}
