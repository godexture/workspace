package audio_test

import (
	"fmt"

	"github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/timing"
)

// Frame carries planar samples and item-local timing; stream-wide rate and
// channel properties live in stream.Descriptor instead.
func ExampleNewFrame() {
	planes, err := buffer.Allocate(buffer.Spec{Planes: []buffer.PlaneSpec{{Size: 4}, {Size: 4}}})
	if err != nil {
		panic(err)
	}
	frame, err := audio.NewFrame[int16](timing.SomePTS(timing.NewPTS(0)), 2, planes)
	if err != nil {
		panic(err)
	}
	defer frame.Release()
	pts, _ := frame.PTS().Get()

	fmt.Println(frame.Samples(), pts)
	fmt.Println(len(frame.Planes().Layout().Planes))
	// Output:
	// 2 0
	// 2
}
