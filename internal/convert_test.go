package cli

import (
	"testing"

	godec "github.com/godexture/core"
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

func TestSelectMuxerParsesConfiguration(t *testing.T) {
	manifest, values, err := selectMuxer("wav:force-rf64=true", "output.flac")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Name != "wav" || values["force-rf64"] != "true" {
		t.Fatalf("selectMuxer() = %#v, %#v", manifest, values)
	}
}

func TestResolvePluginCreatesConfiguration(t *testing.T) {
	manifest, config, err := resolvePlugin("demuxer", "wav", godec.DefaultDemuxerRegistry)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Name != "wav" || config == nil {
		t.Fatalf("resolvePlugin() = %#v, %#v", manifest, config)
	}
}
