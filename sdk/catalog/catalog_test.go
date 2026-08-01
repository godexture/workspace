package catalog_test

import (
	"slices"
	"testing"

	"github.com/godexture/godec/sdk/catalog"

	_ "github.com/godexture/godec/plugin/flac"
	_ "github.com/godexture/godec/plugin/pcm"
	_ "github.com/godexture/godec/plugin/audio"
	_ "github.com/godexture/godec/plugin/wave"
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

func TestDescribeStaticFilterHasEmptyParameterList(t *testing.T) {
	entry, err := catalog.DescribeFilter("gain", nil)
	if err != nil {
		t.Fatalf("DescribeFilter() error = %v", err)
	}
	if entry.Parameters == nil {
		t.Fatal("static filter parameters must be an empty list, not nil")
	}
	if len(entry.Parameters) != 0 {
		t.Fatalf("static filter parameters = %v", entry.Parameters)
	}
}

func TestDescribeFilterEqualizerFieldDependencies(t *testing.T) {
	entry, err := catalog.DescribeFilter("equalizer", nil)
	if err != nil {
		t.Fatalf("DescribeFilter() error = %v", err)
	}
	fields := make(map[string]catalog.Field, len(entry.Fields))
	for _, field := range entry.Fields {
		fields[field.Name] = field
	}
	for _, name := range []string{"type", "frequency-hz", "gain-db", "q"} {
		assertDependency(t, fields[name], "mode", "single")
	}
	for _, name := range []string{"bands", "low-hz", "high-hz", "manual-bands", "gains"} {
		assertDependency(t, fields[name], "mode", "multiband")
	}
	if fields["gains"].Editor != "sliders" {
		t.Fatalf("gains editor = %q", fields["gains"].Editor)
	}
}

func TestResolveEqualizerConfiguration(t *testing.T) {
	resolved, err := catalog.ResolveConfiguration("filter", "equalizer", nil, map[string]string{
		"mode": "multiband", "bands": "3",
	})
	if err != nil {
		t.Fatalf("ResolveConfiguration() error = %v", err)
	}
	if resolved.Values["gains"] != "0,0,0" {
		t.Fatalf("resolved gains = %q", resolved.Values["gains"])
	}
	if resolved.Sources["gains"] != "dynamic" {
		t.Fatalf("gains source = %q", resolved.Sources["gains"])
	}
	if len(resolved.Fields) != 1 || len(resolved.Fields[0].Slots) != 3 {
		t.Fatalf("resolved fields = %#v", resolved.Fields)
	}

	normalized, err := catalog.ResolveConfiguration("filter", "equalizer", nil, map[string]string{
		"mode": "multiband", "bands": "2", "gains": "1,2,3",
	})
	if err != nil {
		t.Fatalf("ResolveConfiguration() normalization error = %v", err)
	}
	if normalized.Updates["gains"] != "1,2" {
		t.Fatalf("normalized gains update = %q", normalized.Updates["gains"])
	}

	manual, err := catalog.ResolveConfiguration("filter", "equalizer", nil, map[string]string{
		"mode": "multiband", "manual-bands": "1000,100",
	})
	if err != nil {
		t.Fatalf("ResolveConfiguration() manual bands error = %v", err)
	}
	slots := manual.Fields[0].Slots
	if len(slots) != 2 ||
		slots[0].Label != "100 Hz" || slots[0].Index != 1 ||
		slots[1].Label != "1 kHz" || slots[1].Index != 0 {
		t.Fatalf("manual band slots = %#v", slots)
	}
}

func assertDependency(t *testing.T, field catalog.Field, name, value string) {
	t.Helper()
	if field.DependsOn == nil || field.DependsOn.Field != name || !slices.Equal(field.DependsOn.Values, []string{value}) {
		t.Fatalf("field %q dependency = %#v", field.Name, field.DependsOn)
	}
}
