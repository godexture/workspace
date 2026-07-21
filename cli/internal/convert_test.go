package cli

import (
	"testing"

	"github.com/godexture/core/domain/media"
)

func TestResolveCodec(t *testing.T) {
	codec, values, err := resolveCodec("flac:preset=2,block-size=1024", media.CodecLPCM)
	if err != nil {
		t.Fatal(err)
	}
	if codec != media.CodecFLAC {
		t.Fatalf("codec = %q", codec)
	}
	if values["preset"] != "2" || values["block-size"] != "1024" {
		t.Fatalf("values = %#v", values)
	}
}

func TestResolveCodecUsesDefault(t *testing.T) {
	codec, values, err := resolveCodec("", media.CodecFLAC)
	if err != nil {
		t.Fatal(err)
	}
	if codec != media.CodecFLAC || values != nil {
		t.Fatalf("resolveCodec() = %q, %#v", codec, values)
	}
}
