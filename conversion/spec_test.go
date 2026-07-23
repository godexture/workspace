package conversion_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"testing"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/pipeline"
	"github.com/godexture/sdk/conversion"

	_ "github.com/godexture/codec-flac"
	_ "github.com/godexture/codec-pcm"
	_ "github.com/godexture/filter-audio"
	_ "github.com/godexture/format-flac"
	_ "github.com/godexture/format-wav"
)

// writeTestWAV writes a short synthetic mono 16-bit PCM WAV (a few hundred
// milliseconds of a 440Hz tone) so pipeline tests do not depend on the large
// shared fixture assets used by core/test.
func writeTestWAV(t *testing.T, buf *bytes.Buffer) {
	t.Helper()
	const sampleRate = 8000
	const numSamples = 4000
	samples := make([]int16, numSamples)
	for i := range samples {
		samples[i] = int16(4000 * math.Sin(2*math.Pi*440*float64(i)/sampleRate))
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

func TestBuildRunsWavToFlac(t *testing.T) {
	var wav bytes.Buffer
	writeTestWAV(t, &wav)

	var flac bytes.Buffer
	built, err := conversion.Build(context.Background(), bytes.NewReader(wav.Bytes()), &flac, conversion.Spec{
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

func errorHasCode(err error, code conversion.Code) bool {
	convErr, ok := err.(*conversion.Error)
	return ok && convErr.Code == code
}
