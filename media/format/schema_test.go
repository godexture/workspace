package format

import (
	"reflect"
	"testing"

	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/timing"
)

func TestChunksSchemaUsesContainerChunkPayload(t *testing.T) {
	typ := Chunks()
	if !typ.Valid() || typ.Descriptor().Payload() != reflect.TypeFor[packet.Chunk]() {
		t.Fatalf("chunk schema = %#v", typ.Descriptor())
	}
	traits := typ.Traits()
	if traits.Fork == nil || traits.Drop == nil || traits.Size == nil || traits.Time == nil {
		t.Fatalf("chunk schema traits = %#v", traits)
	}
}

func TestChunksTimeTraitUsesPTS(t *testing.T) {
	value := packet.NewChunk(
		0,
		timing.SomePTS(timing.NewPTS(7)),
		timing.SomeDTS(timing.NewDTS(3)),
		timing.SomeDuration(timing.NewDuration(2)),
		buffer.Handle{},
	)
	if timestamp, ok := Chunks().Traits().Time(value); !ok || timestamp != 7 {
		t.Fatalf("chunk time trait = %d, valid = %v", timestamp, ok)
	}
}
