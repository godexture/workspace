package packet

import (
	"testing"

	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/timing"
)

func TestChunkAndPacketRemainDistinctAndPreserveTiming(t *testing.T) {
	base := timing.MustBase(1, 1)
	pts := timing.SomePTS(timing.NewPTS(0))
	dts := timing.SomeDTS(timing.NewDTS(-1))
	duration := timing.SomeDuration(timing.NewDuration(2))
	payload, err := buffer.FromBytes([]byte{1, 2, 3}, 8)
	if err != nil {
		t.Fatal(err)
	}
	chunk := NewTimestampedChunk(4, pts, payload)
	packetPayload := chunk.Payload()
	packet := NewPacket(chunk.Sequence(), chunk.PTS(), dts, duration, packetPayload)
	chunk.Release()
	defer packet.Release()
	if !packet.Valid() || packet.PTS().Value() != 0 || packet.DTS().Value() != -1 || packet.Duration().Value() != 2 {
		t.Fatalf("packet timing = %#v", packet)
	}
	if packet.Bytes()[1] != 2 {
		t.Fatal("packet payload was not retained")
	}
	if got, err := packet.PTS().Value().Rescale(base, base, timing.RoundTowardZero); err != nil || got != 0 {
		t.Fatalf("timestamp rescale = %d, %v", got, err)
	}
}

func TestPacketCloneSharesPayload(t *testing.T) {
	payload, err := buffer.FromBytes([]byte{9}, 1)
	if err != nil {
		t.Fatal(err)
	}
	packet := NewPacket(0, timing.UnknownPTS(), timing.UnknownDTS(), timing.UnknownDuration(), payload)
	clone := packet.Clone()
	packet.Release()
	if !clone.Valid() || clone.Bytes()[0] != 9 {
		t.Fatal("clone did not retain payload")
	}
	clone.Release()
}
