package conversion_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"math"
	"testing"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/pipeline"
	"github.com/godexture/godec/sdk/conversion"

	filter "github.com/godexture/godec/plugin/audio"
	_ "github.com/godexture/godec/plugin/flac"
	_ "github.com/godexture/godec/plugin/pcm"
	_ "github.com/godexture/godec/plugin/wave"
)

// writeTestWAV writes a short synthetic mono 16-bit PCM WAV (a few hundred
// milliseconds of a 440Hz tone) so pipeline tests do not depend on the large
// shared fixture assets used by core/test.
func writeTestWAV(t *testing.T, buf *bytes.Buffer) {
	t.Helper()
	writeTestWAVRate(t, buf, 8000)
}

func writeTestWAVRate(t *testing.T, buf *bytes.Buffer, sampleRate int) {
	t.Helper()
	const numSamples = 4000
	samples := make([]int16, numSamples)
	for i := range samples {
		samples[i] = int16(4000 * math.Sin(2*math.Pi*440*float64(i)/float64(sampleRate)))
	}
	dataSize := uint32(len(samples) * 2)

	buf.WriteString("RIFF")
	_ = binary.Write(buf, binary.LittleEndian, uint32(36+dataSize))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	_ = binary.Write(buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(buf, binary.LittleEndian, uint16(1)) // PCM
	_ = binary.Write(buf, binary.LittleEndian, uint16(1)) // mono
	_ = binary.Write(buf, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(buf, binary.LittleEndian, uint32(sampleRate*2)) // byte rate
	_ = binary.Write(buf, binary.LittleEndian, uint16(2))            // block align
	_ = binary.Write(buf, binary.LittleEndian, uint16(16))           // bits per sample
	buf.WriteString("data")
	_ = binary.Write(buf, binary.LittleEndian, dataSize)
	_ = binary.Write(buf, binary.LittleEndian, samples)
}

func TestResolveRequiresMuxerName(t *testing.T) {
	_, err := conversion.Resolve(conversion.Spec{})
	if err == nil {
		t.Fatal("Resolve() with no muxer name did not fail")
	}
	if got := conversion.CodeInvalidSpec; !errorHasCode(err, got) {
		t.Fatalf("Resolve() error code = %v, want %v", err, got)
	}
}

func TestResolveRejectsUnsupportedCodec(t *testing.T) {
	_, err := conversion.Resolve(conversion.Spec{
		Muxer: conversion.PluginSpec{Name: "wav"},
		Codec: "flac",
	})
	if !errorHasCode(err, conversion.CodeUnsupportedCodec) {
		t.Fatalf("Resolve() error = %v, want %s", err, conversion.CodeUnsupportedCodec)
	}
}

func TestResolveDefaultsCodecFromMuxer(t *testing.T) {
	resolved, err := conversion.Resolve(conversion.Spec{Muxer: conversion.PluginSpec{Name: "wav"}})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Codec != media.CodecLPCM {
		t.Fatalf("Resolve() codec = %s, want %s", resolved.Codec, media.CodecLPCM)
	}
}

func TestResolveDecodesFilterValues(t *testing.T) {
	resolved, err := conversion.Resolve(conversion.Spec{
		Muxer:   conversion.PluginSpec{Name: "wav"},
		Filters: []conversion.FilterSpec{{PluginSpec: conversion.PluginSpec{Name: "gain", Values: map[string]string{"decibels": "-6"}}}},
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(resolved.Filters) != 1 {
		t.Fatalf("Resolve() filters = %d, want 1", len(resolved.Filters))
	}
}

func TestResolveFillsDynamicEqualizerDefault(t *testing.T) {
	resolved, err := conversion.Resolve(conversion.Spec{
		Muxer: conversion.PluginSpec{Name: "wav"},
		Filters: []conversion.FilterSpec{{
			PluginSpec: conversion.PluginSpec{
				Name:   "equalizer",
				Values: map[string]string{"mode": "multiband", "bands": "3"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	config, ok := resolved.Filters[0].Config.(*filter.EqualizerConfig)
	if !ok {
		t.Fatalf("Resolve() config = %T, want *filter.EqualizerConfig", resolved.Filters[0].Config)
	}
	if config.Gains != "0,0,0" {
		t.Fatalf("Resolve() gains = %q, want %q", config.Gains, "0,0,0")
	}
}

func TestResolveRejectsExplicitEqualizerGainCount(t *testing.T) {
	_, err := conversion.Resolve(conversion.Spec{
		Muxer: conversion.PluginSpec{Name: "wav"},
		Filters: []conversion.FilterSpec{{
			PluginSpec: conversion.PluginSpec{
				Name:   "equalizer",
				Values: map[string]string{"mode": "multiband", "bands": "3", "gains": "0,0"},
			},
		}},
	})
	if !errorHasCode(err, conversion.CodeInvalidSpec) {
		t.Fatalf("Resolve() error = %v, want %s", err, conversion.CodeInvalidSpec)
	}
}

func TestResolveDecodesRemixLayout(t *testing.T) {
	_, err := conversion.Resolve(conversion.Spec{
		Muxer: conversion.PluginSpec{Name: "wav"},
		Filters: []conversion.FilterSpec{{
			PluginSpec: conversion.PluginSpec{Name: "remix", Values: map[string]string{"layout": "stereo"}},
		}},
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
}

func TestBuildRunsWavToFlac(t *testing.T) {
	var wav bytes.Buffer
	writeTestWAV(t, &wav)

	var flac bytes.Buffer
	built, err := conversion.Build(context.Background(), conversion.InputSet{Main: bytes.NewReader(wav.Bytes())}, &flac, conversion.Spec{
		Muxer: conversion.PluginSpec{Name: "flac"},
		Codec: string(media.CodecFLAC),
	}, pipeline.ObservationProgress)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	defer built.Close()

	// Regression guard: NodeDescription.Configuration must stay excluded
	// from JSON (json:"-") since plugin config types are free to hold
	// non-marshalable values -- codec-flac's EncoderConfig.Apodizations is
	// []func([]float64), which made this fail with "unsupported type"
	// before Configuration was excluded.
	if _, err := json.Marshal(built.Description()); err != nil {
		t.Fatalf("json.Marshal(Description()) error = %v", err)
	}

	if err := built.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if flac.Len() == 0 {
		t.Fatal("Run() produced no output")
	}

	progress := conversion.Snapshot(built.Snapshot(), true)
	if progress.Percent != 100 {
		t.Fatalf("Snapshot() percent = %v, want 100", progress.Percent)
	}
	if len(progress.Nodes) == 0 {
		t.Fatal("Snapshot() reported no nodes")
	}
}

func TestBuildPreloadsNamedAuxiliaryInput(t *testing.T) {
	var mainWAV, impulseWAV, output bytes.Buffer
	writeTestWAV(t, &mainWAV)
	writeTestWAV(t, &impulseWAV)

	built, err := conversion.Build(context.Background(), conversion.InputSet{
		Main: bytes.NewReader(mainWAV.Bytes()),
		Aux:  map[string]io.ReadSeeker{"IR": bytes.NewReader(impulseWAV.Bytes())},
	}, &output, conversion.Spec{
		Muxer: conversion.PluginSpec{Name: "wav"},
		Filters: []conversion.FilterSpec{{
			PluginSpec: conversion.PluginSpec{Name: "convolver", Values: map[string]string{"wet-dry-mix": "1"}},
			Inputs:     map[string]conversion.PortRef{"ir": {Alias: "IR"}},
		}},
	}, pipeline.ObservationOff)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	defer built.Close()
	if err := built.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if output.Len() == 0 {
		t.Fatal("Run() produced no output")
	}
}

// TestBuildSplitsStreamThroughReverbAndDelayThenMixes exercises the graph
// negotiator's fan-out/fan-in support: "split" (a 1-in/2-out mixer, i.e. a
// tee) forks the main stream, "reverb" and "delay" each process one branch,
// and "join" (a 2-in/1-out mixer) mixes them back into the single stream the
// encoder reads. Since a mixer's ports are always "in0".."out0".. rather
// than the conventional "in"/"out", split's "in0" and join's output both
// need explicit wiring (join.out0 via Spec.Sink) instead of the declaration-
// order default chain that plain single-port filters get for free.
func TestBuildSplitsStreamThroughReverbAndDelayThenMixes(t *testing.T) {
	var mainWAV, output bytes.Buffer
	writeTestWAV(t, &mainWAV)

	built, err := conversion.Build(context.Background(), conversion.InputSet{
		Main: bytes.NewReader(mainWAV.Bytes()),
	}, &output, conversion.Spec{
		Muxer: conversion.PluginSpec{Name: "wav"},
		Filters: []conversion.FilterSpec{
			{
				PluginSpec: conversion.PluginSpec{Name: "mixer"},
				Alias:      "split",
				Parameters: map[string]string{"in": "1", "out": "2"},
				Inputs:     map[string]conversion.PortRef{"in0": {Alias: conversion.MainInputAlias}},
			},
			{
				PluginSpec: conversion.PluginSpec{Name: "reverb"},
				Alias:      "reverb",
				Inputs:     map[string]conversion.PortRef{"in": {Alias: "split", Port: "out0"}},
			},
			{
				PluginSpec: conversion.PluginSpec{Name: "delay"},
				Alias:      "delay",
				Inputs:     map[string]conversion.PortRef{"in": {Alias: "split", Port: "out1"}},
			},
			{
				PluginSpec: conversion.PluginSpec{Name: "mixer"},
				Alias:      "join",
				Parameters: map[string]string{"in": "2", "out": "1"},
				Inputs: map[string]conversion.PortRef{
					"in0": {Alias: "reverb"},
					"in1": {Alias: "delay"},
				},
			},
		},
		Sink: &conversion.PortRef{Alias: "join", Port: "out0"},
	}, pipeline.ObservationOff)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	defer built.Close()
	if err := built.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if output.Len() == 0 {
		t.Fatal("Run() produced no output")
	}
}

func TestNegotiateResamplesConvolutionImpulseResponseToMainRate(t *testing.T) {
	var mainWAV, impulseWAV, output bytes.Buffer
	writeTestWAVRate(t, &mainWAV, 8000)
	writeTestWAVRate(t, &impulseWAV, 16000)

	geometry, err := conversion.Negotiate(context.Background(), conversion.InputSet{
		Main: bytes.NewReader(mainWAV.Bytes()),
		Aux:  map[string]io.ReadSeeker{"ir": bytes.NewReader(impulseWAV.Bytes())},
	}, &output, conversion.Spec{
		Muxer: conversion.PluginSpec{Name: "wav"},
		Filters: []conversion.FilterSpec{{
			PluginSpec: conversion.PluginSpec{Name: "convolver"},
			Inputs:     map[string]conversion.PortRef{"ir": {Alias: "ir"}},
		}},
	})
	if err != nil {
		t.Fatalf("Negotiate() error = %v", err)
	}
	defer geometry.Close()

	for _, node := range geometry.Description().Nodes {
		if node.Plugin == "resample" && node.AutoInserted && len(node.Outputs) == 1 && node.Outputs[0].Audio.SampleRate == 8000 {
			return
		}
	}
	t.Fatalf("pipeline did not insert an auxiliary resampler to 8000 Hz: %#v", geometry.Description().Nodes)
}

func errorHasCode(err error, code conversion.Code) bool {
	convErr, ok := err.(*conversion.Error)
	return ok && convErr.Code == code
}
