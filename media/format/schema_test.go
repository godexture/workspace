package format

import (
	"reflect"
	"testing"

	"github.com/godexture/godec/media/packet"
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
