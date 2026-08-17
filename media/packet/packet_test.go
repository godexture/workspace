package packet

import (
	"testing"

	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/media/side"
	"github.com/godexture/godec/media/timing"
)

type packetSideKey struct{}

func packetAllocator(t *testing.T) *buffer.Allocator {
	t.Helper()
	allocator, err := buffer.NewAllocator(1024)
	if err != nil {
		t.Fatal(err)
	}
	return allocator
}

func TestChunkAndPacketRemainDistinctAndPreserveTiming(t *testing.T) {
	base := timing.MustBase(1, 1)
	pts := timing.SomePTS(timing.NewPTS(0))
	dts := timing.SomeDTS(timing.NewDTS(-1))
	duration := timing.SomeDuration(timing.NewDuration(2))
	payload, err := packetAllocator(t).FromBytes([]byte{1, 2, 3}, 8)
	if err != nil {
		t.Fatal(err)
	}
	chunk := NewChunk(4, pts, dts, duration, payload)
	packetPayload := chunk.Payload().Share()
	packet := NewPacket(chunk.Sequence(), chunk.PTS(), chunk.DTS(), chunk.Duration(), packetPayload)
	chunk.Release()
	defer packet.Release()
	if !packet.Valid() || packet.PTS().Value() != 0 || packet.DTS().Value() != -1 || packet.Duration().Value() != 2 {
		t.Fatalf("packet timing = %#v", packet)
	}
	if packet.Bytes().At(1) != 2 {
		t.Fatal("packet payload was not retained")
	}
	if got, err := packet.PTS().Value().Rescale(base, base, timing.RoundTowardZero); err != nil || got != 0 {
		t.Fatalf("timestamp rescale = %d, %v", got, err)
	}
}

func TestPacketShareRetainsPayload(t *testing.T) {
	payload, err := packetAllocator(t).FromBytes([]byte{9}, 1)
	if err != nil {
		t.Fatal(err)
	}
	packet := NewPacket(0, timing.UnknownPTS(), timing.UnknownDTS(), timing.UnknownDuration(), payload)
	clone := packet.Share()
	packet.Release()
	if !clone.Valid() || clone.Bytes().At(0) != 9 {
		t.Fatal("clone did not retain payload")
	}
	clone.Release()
}

func TestChunkUnknownTimingStaysDistinctFromKnownZero(t *testing.T) {
	unknown := NewChunk(0, timing.UnknownPTS(), timing.UnknownDTS(), timing.UnknownDuration(), buffer.Handle{})
	zero := NewChunk(0, timing.SomePTS(timing.NewPTS(0)), timing.SomeDTS(timing.NewDTS(0)), timing.SomeDuration(timing.NewDuration(0)), buffer.Handle{})
	if unknown.PTS().Valid() || unknown.DTS().Valid() || unknown.Duration().Valid() {
		t.Fatal("unknown chunk timing became valid")
	}
	if !zero.PTS().Valid() || !zero.DTS().Valid() || !zero.Duration().Valid() {
		t.Fatal("known zero chunk timing became unknown")
	}
}

func TestChunkPayloadIsBorrowed(t *testing.T) {
	payload, err := packetAllocator(t).FromBytes([]byte{7}, 1)
	if err != nil {
		t.Fatal(err)
	}
	chunk := NewChunk(0, timing.UnknownPTS(), timing.UnknownDTS(), timing.UnknownDuration(), payload)
	view := chunk.Payload()
	chunk.Release()
	if view.Valid() {
		t.Fatal("chunk payload accessor retained storage implicitly")
	}
}

func TestPacketCarriesImmutableSideData(t *testing.T) {
	key := key.Define[packetSideKey, string]()
	data, err := side.Add(side.Data{}, key, "frame")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := packetAllocator(t).FromBytes([]byte{1}, 1)
	if err != nil {
		t.Fatal(err)
	}
	packet := NewPacket(0, timing.UnknownPTS(), timing.UnknownDTS(), timing.UnknownDuration(), payload).WithSideData(data)
	defer packet.Release()
	clone := packet.Share()
	defer clone.Release()
	if value, ok := side.First(clone.SideData(), key); !ok || value != "frame" {
		t.Fatalf("packet side data = %q, %v", value, ok)
	}
}
