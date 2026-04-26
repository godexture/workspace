// core/domain/media/frame_audio.go
package media

import (
	"unsafe"

	"github.com/godexture/core/domain/metadata"
)

type SampleType interface {
	~uint8 | ~int16 | ~int32 | ~float32 | ~float64
}

type AudioFrame struct {
	ResourceBase
	baseData *[]byte

	Format     SampleFormat
	Layout     ChannelLayout
	SampleRate int
	Samples    int
	Metadata   *metadata.Bundle

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
