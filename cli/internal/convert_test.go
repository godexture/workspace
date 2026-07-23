package cli

import (
	"testing"

	"github.com/godexture/sdk/catalog"
)

func TestParsePluginSpecParsesNameAndValues(t *testing.T) {
	spec, err := parsePluginSpec("flac:preset=2,block-size=1024")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Name != "flac" || spec.Values["preset"] != "2" || spec.Values["block-size"] != "1024" {
		t.Fatalf("parsePluginSpec() = %#v", spec)
	}
}

func TestParsePluginSpecEmptyValueIsNil(t *testing.T) {
	spec, err := parsePluginSpec("")
	if err != nil {
		t.Fatal(err)
	}
	if spec != nil {
		t.Fatalf("parsePluginSpec(\"\") = %#v, want nil", spec)
	}
}

func TestInferMuxerNameMatchesExtension(t *testing.T) {
	outputs := catalog.Build().Outputs
	name, err := inferMuxerName(outputs, "output.flac")
	if err != nil {
		t.Fatal(err)
	}
	if name != "flac" {
		t.Fatalf("inferMuxerName() = %q, want %q", name, "flac")
	}
}

func TestInferMuxerNameRequiresFormatWhenUnknown(t *testing.T) {
	outputs := catalog.Build().Outputs
	if _, err := inferMuxerName(outputs, "output.xyz"); err == nil {
		t.Fatal("inferMuxerName() accepted an unknown extension")
	}
}

func TestBuildSpecUsesExplicitFormatOverExtension(t *testing.T) {
	outputs := catalog.Build().Outputs
	spec, err := buildSpec(convertOptions{format: "wav:force-rf64=true"}, "output.flac", outputs)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Muxer.Name != "wav" || spec.Muxer.Values["force-rf64"] != "true" {
		t.Fatalf("buildSpec() muxer = %#v", spec.Muxer)
	}
}

func TestBuildSpecAppliesCodecValuesWithoutOverridingEncoderSelection(t *testing.T) {
	outputs := catalog.Build().Outputs
	spec, err := buildSpec(convertOptions{codec: "flac:preset=2"}, "output.flac", outputs)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Codec != "flac" {
		t.Fatalf("buildSpec() codec = %q", spec.Codec)
	}
	if spec.Encoder == nil || spec.Encoder.Name != "" || spec.Encoder.Values["preset"] != "2" {
		t.Fatalf("buildSpec() encoder = %#v", spec.Encoder)
	}
}
