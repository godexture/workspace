package cli

import (
	"testing"

	"github.com/godexture/godec/sdk/catalog"
	"github.com/godexture/godec/sdk/conversion"
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

// TestBuildSpecResolvesParameterizedMixerFilter exercises the full
// "mixer[in=2,out=1]" CLI path end to end: buildSpec parses the bracket
// segment into FilterSpec.Parameters, and conversion.Resolve looks "mixer"
// up in the parameterized registry, resolves MixerParameters from those
// values, and uses it to build the concrete FilterManifest before
// resolving MixerConfig.
func TestBuildSpecResolvesParameterizedMixerFilter(t *testing.T) {
	outputs := catalog.Build().Outputs
	spec, err := buildSpec(convertOptions{
		filters: []string{"mixer[in=2,out=1]"},
	}, "output.wav", outputs)
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Filters) != 1 || spec.Filters[0].Name != "mixer" {
		t.Fatalf("buildSpec() filters = %#v", spec.Filters)
	}
	if spec.Filters[0].Parameters["in"] != "2" || spec.Filters[0].Parameters["out"] != "1" {
		t.Fatalf("buildSpec() parameters = %#v", spec.Filters[0].Parameters)
	}

	resolved, err := conversion.Resolve(spec)
	if err != nil {
		t.Fatalf("conversion.Resolve() error = %v", err)
	}
	if len(resolved.Filters) != 1 {
		t.Fatalf("resolved filters = %#v", resolved.Filters)
	}
}

func TestBuildSpecRejectsParametersOnNonParameterizedFilter(t *testing.T) {
	outputs := catalog.Build().Outputs
	spec, err := buildSpec(convertOptions{
		filters: []string{"gain[foo=bar]"},
	}, "output.wav", outputs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conversion.Resolve(spec); err == nil {
		t.Fatal("conversion.Resolve() accepted parameters on a non-parameterized filter")
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
		filters: []string{"reverb=convolver:wet-dry-mix=1"},
		inputs:  []string{"IR=cabinet.wav"},
		wires:   []string{"reverb.ir=IR"},
	}, "output.wav", outputs)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := spec.AuxInputs["IR"]; !ok {
		t.Fatalf("buildSpec() auxiliary inputs = %#v", spec.AuxInputs)
	}
	if got, want := spec.Filters[0].Inputs["ir"], (conversion.PortRef{Alias: "IR", Port: "out"}); got != want {
		t.Fatalf("buildSpec() ir wire = %#v, want %#v", got, want)
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

// TestBuildSpecWiresFilterChainAheadOfAnotherFilter exercises wiring one
// filter's output into another's non-"in" port through an intermediate
// filter: "resample" reads the "ir" auxiliary input, and "convolver" reads
// resample's output on its "ir" port. Unlike the old model, resample is not
// nested under AuxInputs  Eit is an ordinary entry in spec.Filters, wired
// like anything else; the graph is resolved uniformly by
// conversion.Resolve/routing.NegotiateConversion.
func TestBuildSpecWiresFilterChainAheadOfAnotherFilter(t *testing.T) {
	spec, err := buildSpec(convertOptions{
		filters: []string{"resample=resample:sample-rate=48000", "convolver"},
		inputs:  []string{"ir=cabinet.wav"},
		wires: []string{
			"resample.in=ir.out",
			"convolver.ir=resample.out",
		},
	}, "output.wav", catalog.Build().Outputs)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := spec.AuxInputs["ir"]; !ok {
		t.Fatalf("buildSpec() auxiliary inputs = %#v", spec.AuxInputs)
	}
	if len(spec.Filters) != 2 || spec.Filters[0].Alias != "resample" || spec.Filters[1].Alias != "convolver" {
		t.Fatalf("buildSpec() filters = %#v", spec.Filters)
	}
	if got, want := spec.Filters[0].Inputs["in"], (conversion.PortRef{Alias: "ir", Port: "out"}); got != want {
		t.Fatalf("buildSpec() resample in source = %#v, want %#v", got, want)
	}
	if got, want := spec.Filters[1].Inputs["ir"], (conversion.PortRef{Alias: "resample", Port: "out"}); got != want {
		t.Fatalf("buildSpec() convolver ir source = %#v, want %#v", got, want)
	}
}

// TestBuildSpecSplitsMainStreamThroughReverbAndDelayThenMixes exercises the
// CLI syntax for forking the main stream through two filters and mixing the
// branches back: "split" is a 1-in/2-out mixer (a tee), "join" is a
// 2-in/1-out mixer. Neither has a literal "in"/"out" port, so both need
// explicit wiring  Eincluding @in as split's source and @out as join's
// destination  Einstead of the declaration-order default chain plain
// single-port filters (reverb, delay) get for free.
func TestBuildSpecSplitsMainStreamThroughReverbAndDelayThenMixes(t *testing.T) {
	spec, err := buildSpec(convertOptions{
		filters: []string{"split=mixer[in=1,out=2]", "reverb", "delay", "join=mixer[in=2,out=1]"},
		wires: []string{
			"split.in0=@in",
			"reverb.in=split.out0",
			"delay.in=split.out1",
			"join.in0=reverb.out",
			"join.in1=delay.out",
			"@out.in=join.out0",
		},
	}, "output.wav", catalog.Build().Outputs)
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Filters) != 4 {
		t.Fatalf("buildSpec() filters = %#v", spec.Filters)
	}
	if got, want := spec.Filters[0].Inputs["in0"], (conversion.PortRef{Alias: conversion.MainInputAlias, Port: "out"}); got != want {
		t.Fatalf("buildSpec() split.in0 = %#v, want %#v", got, want)
	}
	if got, want := spec.Filters[3].Inputs["in0"], (conversion.PortRef{Alias: "reverb", Port: "out"}); got != want {
		t.Fatalf("buildSpec() join.in0 = %#v, want %#v", got, want)
	}
	if spec.Sink == nil {
		t.Fatal("buildSpec() sink = nil, want join.out0")
	}
	if got, want := *spec.Sink, (conversion.PortRef{Alias: "join", Port: "out0"}); got != want {
		t.Fatalf("buildSpec() sink = %#v, want %#v", got, want)
	}

	if _, err := conversion.Resolve(spec); err != nil {
		t.Fatalf("conversion.Resolve() error = %v", err)
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
