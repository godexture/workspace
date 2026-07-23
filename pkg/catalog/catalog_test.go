package catalog_test

import (
	"slices"
	"testing"

	"github.com/godexture/sdk/catalog"

	_ "github.com/godexture/codec-flac"
	_ "github.com/godexture/codec-pcm"
	_ "github.com/godexture/format-flac"
	_ "github.com/godexture/format-wav"
)

func TestBuildListsPluginsAndSupportedOutputs(t *testing.T) {
	value := catalog.Build()
	if len(value.Encoders) == 0 || len(value.Muxers) == 0 {
		t.Fatalf("catalog has %d encoders and %d muxers", len(value.Encoders), len(value.Muxers))
	}

	var wavFound, flacFound bool
	for _, output := range value.Outputs {
		switch output.Muxer {
		case "wav":
			wavFound = slices.Contains(output.Extensions, ".wav") && slices.Contains(output.Codecs, "lpcm")
		case "flac":
			flacFound = slices.Contains(output.Extensions, ".flac") && slices.Contains(output.Codecs, "flac")
		}
	}
	if !wavFound || !flacFound {
		t.Fatalf("supported outputs missing: wav=%t flac=%t, outputs=%#v", wavFound, flacFound, value.Outputs)
	}
}
