package testkit

import (
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/media/codec"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
)

func TestExplicitMediaFixturesDoNotRequirePCMProperties(t *testing.T) {
	chunksDescriptor := stream.MustDescriptor("chunks", mediaformat.Chunks().Descriptor(), timing.MustBase(1, 1), property.New())
	chunks := ChunkInputFor(chunksDescriptor, []Chunk{{Bytes: []byte{1, 2}}})
	if !chunks.valid() {
		t.Fatal("explicit chunk fixture is invalid")
	}
	if err := chunks.close(); err != nil {
		t.Fatal(err)
	}

	packetsDescriptor := stream.MustDescriptor("packets", codec.Packets().Descriptor(), timing.MustBase(1, 1), property.New())
	packets := PacketInputFor(packetsDescriptor, []Packet{{Bytes: []byte{3, 4}}})
	if !packets.valid() {
		t.Fatal("explicit packet fixture is invalid")
	}
	if err := packets.close(); err != nil {
		t.Fatal(err)
	}

	wrong := stream.MustDescriptor("bytes", access.Bytes().Descriptor(), timing.Base{}, property.New())
	if ChunkInputFor(wrong, nil).valid() || PacketInputFor(wrong, nil).valid() {
		t.Fatal("explicit media fixtures accepted the wrong schema")
	}
}
