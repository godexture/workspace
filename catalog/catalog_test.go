package catalog_test

import (
	"slices"
	"testing"

	"github.com/godexture/sdk/catalog"

	_ "github.com/godexture/codec-flac"
	_ "github.com/godexture/codec-pcm"
	_ "github.com/godexture/filter-audio"
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

func TestDescribeParameterizedMixerTopology(t *testing.T) {
	entry, err := catalog.DescribeFilter("mixer", map[string]string{"in": "2", "out": "3"})
	if err != nil {
		t.Fatalf("DescribeFilter() error = %v", err)
	}
	if !slices.Equal(entry.Inputs, []string{"in0", "in1"}) {
		t.Fatalf("mixer inputs = %v", entry.Inputs)
	}
	if !slices.Equal(entry.Outputs, []string{"out0", "out1", "out2"}) {
		t.Fatalf("mixer outputs = %v", entry.Outputs)
	}
	if len(entry.Parameters) != 2 {
		t.Fatalf("mixer parameter fields = %v", entry.Parameters)
	}
}
