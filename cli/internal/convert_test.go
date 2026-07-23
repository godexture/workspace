package cli

import (
	"testing"

	"github.com/godexture/sdk/catalog"
)

func TestParsePluginSpecParsesNameAndValues(t *testing.T) {
	spec, _, err := parsePluginSpec("flac:preset=2,block-size=1024")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Name != "flac" || spec.Values["preset"] != "2" || spec.Values["block-size"] != "1024" {
		t.Fatalf("parsePluginSpec() = %#v", spec)
	}
}

func TestParsePluginSpecEmptyValueIsNil(t *testing.T) {
	spec, _, err := parsePluginSpec("")
	if err != nil {
		t.Fatal(err)
	}
	if spec != nil {
		t.Fatalf("parsePluginSpec(\"\") = %#v, want nil", spec)
	}
}

func TestParseFilterSpecWithParameters(t *testing.T) {
	alias, plugin, parameters, err := parseFilterSpec("mixer[in=2,out=1]:normalize=true")
	if err != nil {
		t.Fatal(err)
	}
	if alias != "" || plugin.Name != "mixer" || plugin.Values["normalize"] != "true" {
		t.Fatalf("parseFilterSpec() = alias=%q plugin=%#v", alias, plugin)
	}
	if parameters["in"] != "2" || parameters["out"] != "1" {
		t.Fatalf("parseFilterSpec() parameters = %#v", parameters)
	}
}

// TestParseFilterSpecAliasWithParameters guards against a specific bug: a
// naive "first '=' before the first ':'" alias split would mistake the
// '=' inside "[in=2]" for the alias separator when there's no ':' segment
// at all. The alias's '=' must only be looked for in the name region,
// before any '[' or ':'.
func TestParseFilterSpecAliasWithParameters(t *testing.T) {
	alias, plugin, parameters, err := parseFilterSpec("myalias=mixer[in=2,out=1]:normalize=true")
	if err != nil {
		t.Fatal(err)
	}
	if alias != "myalias" || plugin.Name != "mixer" || plugin.Values["normalize"] != "true" {
		t.Fatalf("parseFilterSpec() = alias=%q plugin=%#v", alias, plugin)
	}
	if parameters["in"] != "2" || parameters["out"] != "1" {
		t.Fatalf("parseFilterSpec() parameters = %#v", parameters)
	}
}

func TestParseFilterSpecParametersWithoutColon(t *testing.T) {
	alias, plugin, parameters, err := parseFilterSpec("mixer[in=2,out=1]")
	if err != nil {
		t.Fatal(err)
	}
	if alias != "" || plugin.Name != "mixer" || len(plugin.Values) != 0 {
		t.Fatalf("parseFilterSpec() = alias=%q plugin=%#v", alias, plugin)
	}
	if parameters["in"] != "2" || parameters["out"] != "1" {
		t.Fatalf("parseFilterSpec() parameters = %#v", parameters)
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

func TestBuildSpecWiresNamedAuxiliaryInput(t *testing.T) {
	outputs := catalog.Build().Outputs
	spec, err := buildSpec(convertOptions{
		filters: []string{"reverb=convolve:wet-dry-mix=1"},
		inputs:  []string{"IR=cabinet.wav"},
		wires:   []string{"reverb.ir=IR"},
	}, "output.wav", outputs)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := spec.AuxInputs["IR"]; !ok {
		t.Fatalf("buildSpec() auxiliary inputs = %#v", spec.AuxInputs)
	}
	if got := spec.Filters[0].Inputs["ir"]; got != "IR" {
		t.Fatalf("buildSpec() ir wire = %q, want IR", got)
	}
}

func TestBuildSpecRejectsAmbiguousDefaultFilterAlias(t *testing.T) {
	_, err := buildSpec(convertOptions{
		filters: []string{"gain", "gain"},
		inputs:  []string{"IR=cabinet.wav"},
		wires:   []string{"gain.ir=IR"},
	}, "output.wav", catalog.Build().Outputs)
	if err == nil {
		t.Fatal("buildSpec() accepted an ambiguous filter alias")
	}
}

func TestBuildSpecBuildsAuxiliaryFilterChain(t *testing.T) {
	spec, err := buildSpec(convertOptions{
		filters: []string{"resample=resample:sample-rate=48000", "convolve"},
		inputs:  []string{"ir=cabinet.wav"},
		wires: []string{
			"resample.in=ir.out",
			"convolve.ir=resample.out",
		},
	}, "output.wav", catalog.Build().Outputs)
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Filters) != 1 || spec.Filters[0].Alias != "convolve" {
		t.Fatalf("buildSpec() main filters = %#v", spec.Filters)
	}
	auxiliary := spec.AuxInputs["ir"]
	if len(auxiliary.Filters) != 1 || auxiliary.Filters[0].Alias != "resample" {
		t.Fatalf("buildSpec() auxiliary filters = %#v", auxiliary.Filters)
	}
	if got := spec.Filters[0].Inputs["ir"]; got != "ir" {
		t.Fatalf("buildSpec() convolve ir source = %q, want ir", got)
	}
}

func TestBuildSpecRejectsInputAndFilterAliasCollision(t *testing.T) {
	_, err := buildSpec(convertOptions{
		filters: []string{"ir=resample"},
		inputs:  []string{"ir=cabinet.wav"},
	}, "output.wav", catalog.Build().Outputs)
	if err == nil {
		t.Fatal("buildSpec() accepted matching input and filter aliases")
	}
}
