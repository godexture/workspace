package packet_test

import (
	"fmt"

	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/timing"
)

// Packet distinguishes a known zero timestamp from an unknown one and owns
// its payload until Release.
func ExampleNewPacket() {
	payload, err := buffer.FromBytes([]byte{0xaa, 0xbb}, 1)
	if err != nil {
		panic(err)
	}
	value := packet.NewPacket(
		3,
		timing.SomePTS(timing.NewPTS(0)),
		timing.UnknownDTS(),
		timing.SomeDuration(timing.NewDuration(1024)),
		payload,
	)
	defer value.Release()
	pts, ptsKnown := value.PTS().Get()
	_, dtsKnown := value.DTS().Get()

	fmt.Println(value.Sequence(), pts, ptsKnown, dtsKnown)
	fmt.Printf("%x\n", value.Bytes())
	// Output:
	// 3 0 true false
	// aabb
}
