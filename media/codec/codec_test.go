package codec

import (
	"context"
	"testing"

	"github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/packet"
)

func TestBindingKeepsParserIndependentFromFormat(t *testing.T) {
	parser := NewParser("fixture:parser", func(context.Context, packet.Chunk) ([]packet.Packet, error) { return nil, nil })
	binding := Bind(format.NewTag("fixture", "tag"), New("fixture:codec"), parser)
	if !binding.Valid() || binding.Target().Codec != "fixture:codec" {
		t.Fatalf("binding = %#v", binding)
	}
	if got, ok := binding.Parser(); !ok || got.Identity() != "fixture:parser" {
		t.Fatalf("parser = %#v, %v", got, ok)
	}
}
