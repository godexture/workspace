package media

import "unsafe"

type SampleType interface {
	~uint8 | ~int16 | ~int32 | ~float32 | ~float64
}

type AudioFrame struct {
	ResourceBase
	baseData *[]byte

	Format SampleFormat
	// BitsPerSample is the significant sample width carried by this frame.
	// It may differ from the container's nominal format width (for example,
	// FLAC 12-bit samples are carried in an S16 frame).
	BitsPerSample int
	Layout        ChannelLayout
	SampleRate    int
	Samples       int

	pts    Pts
	planes [][]byte
}

func (f *AudioFrame) Pts() Pts { return f.pts }

func (f *AudioFrame) Planes() [][]byte { return f.planes }

func Plane[T SampleType](f *AudioFrame, planeIndex int) []T {
	if planeIndex >= len(f.planes) {
		return nil
	}

	bytes := f.planes[planeIndex]
	if len(bytes) == 0 {
		return nil
	}

	ptr := (*T)(unsafe.Pointer(&bytes[0]))

	length := len(bytes) / int(unsafe.Sizeof(*ptr))

	return unsafe.Slice(ptr, length)
}
