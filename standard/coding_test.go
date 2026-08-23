package standard_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/format"
	"github.com/godexture/godec/standard"
)

// Asking for an output coding the input does not use is what makes a graph
// span two frame representations: the decoder produces the coding its wire
// stores, the encoder consumes the one its wire needs, and only a converter
// joins them. Without it the request has no plan at all.
func TestConvertToARequestedCodingCrossesFrameRepresentations(t *testing.T) {
	samples := []float32{-1, -0.5, 0, 0.25, 1}
	payload := make([]byte, len(samples)*4)
	for index, value := range samples {
		binary.LittleEndian.PutUint32(payload[index*4:], math.Float32bits(value))
	}
	source := waveFile(waveShape{channels: 1, bits: 32, float: true}, payload)

	directory := t.TempDir()
	input := filepath.Join(directory, "input.wav")
	output := filepath.Join(directory, "output.wav")
	if err := os.WriteFile(input, source, 0o644); err != nil {
		t.Fatal(err)
	}
	instance, err := standard.NewHost()
	if err != nil {
		t.Fatal(err)
	}
	extension, err := format.ParseExtension("wav")
	if err != nil {
		t.Fatal(err)
	}
	selector, err := job.SelectFormatExtension(extension)
	if err != nil {
		t.Fatal(err)
	}
	request, err := standard.NewFileJob(input, output,
		standard.WithOutputFormat(selector.WithConfig(config.NewPatch().SetText("coding", "s16"))))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := instance.Plan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := instance.Run(context.Background(), request); err != nil {
		t.Fatal(err)
	}

	converters := 0
	for _, node := range compiled.Nodes() {
		for _, effect := range node.Effects {
			if effect.Detail == "audio.sample-conversion" {
				converters++
			}
		}
	}
	if converters != 1 {
		t.Fatalf("sample conversions in the plan = %d, want 1", converters)
	}

	converted, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	want := []int16{-32768, -16384, 0, 8192, 32767}
	data := make([]byte, len(want)*2)
	for index, value := range want {
		binary.LittleEndian.PutUint16(data[index*2:], uint16(value))
	}
	if !bytes.Contains(converted, data) {
		t.Fatalf("converted payload = %x, want it to carry %x", converted, data)
	}
	if !bytes.Contains(converted, waveFormat(waveShape{channels: 1, bits: 16}, 2)) {
		t.Fatalf("converted header does not declare 16-bit PCM: %x", converted)
	}
}

// A companded stream states a signal and no storage representation, so nothing
// in the graph can read its samples yet. Copying it has to work anyway: the
// default is to keep what the input carried, and a stream whose samples are
// opaque here is exactly the one that must not be rewritten.
func TestConvertCopiesACompandedStream(t *testing.T) {
	for name, shape := range map[string]waveShape{
		"mu-law mono":  {channels: 1, bits: 8, formatTag: 7},
		"a-law stereo": {channels: 2, bits: 8, formatTag: 6},
	} {
		t.Run(name, func(t *testing.T) {
			payload := make([]byte, 64*shape.channels)
			for index := range payload {
				payload[index] = byte(index * 7)
			}
			source := waveFile(shape, payload)
			directory := t.TempDir()
			input := filepath.Join(directory, "input.wav")
			output := filepath.Join(directory, "output.wav")
			if err := os.WriteFile(input, source, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := standard.Convert(context.Background(), input, output); err != nil {
				t.Fatal(err)
			}
			converted, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(converted, payload) {
				t.Fatalf("converted %d bytes do not carry the input payload", len(converted))
			}
			format := waveFormat(shape, shape.channels*shape.bits/8)
			chunk := append(append([]byte("fmt "), sizeOf(len(format))...), format...)
			if !bytes.Contains(converted, chunk) {
				t.Fatalf("converted format header does not match the source:\n got %x\nwant %x", converted, chunk)
			}
			// RIFF states a sample count for every stream that is not plain
			// PCM, and a newly built header has to carry one.
			if !bytes.Contains(converted, []byte("fact")) {
				t.Fatalf("companded output carries no sample-count chunk: %x", converted)
			}
		})
	}
}

// Asking for a linear coding on a companded stream is the case the muxer
// cannot describe in advance: the depth decoding recovers belongs to the
// codec, not to the header. It states the condition instead, and the planner
// closes the gap by finding a path whose result the muxer accepts.
func TestConvertRewritesACompandedStreamIntoLinearPCM(t *testing.T) {
	coded := []byte{0x00, 0x80, 0xff, 0x7f, 0x10, 0x90}
	source := waveFile(waveShape{channels: 1, bits: 8, formatTag: 7}, coded)
	directory := t.TempDir()
	input := filepath.Join(directory, "input.wav")
	output := filepath.Join(directory, "output.wav")
	if err := os.WriteFile(input, source, 0o644); err != nil {
		t.Fatal(err)
	}
	instance, err := standard.NewHost()
	if err != nil {
		t.Fatal(err)
	}
	extension, err := format.ParseExtension("wav")
	if err != nil {
		t.Fatal(err)
	}
	selector, err := job.SelectFormatExtension(extension)
	if err != nil {
		t.Fatal(err)
	}
	request, err := standard.NewFileJob(input, output,
		standard.WithOutputFormat(selector.WithConfig(config.NewPatch().SetText("coding", "s16"))))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := instance.Plan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	expansions := 0
	for _, node := range compiled.Nodes() {
		for _, effect := range node.Effects {
			if effect.Detail == "g711.expand" {
				expansions++
			}
		}
	}
	if expansions != 1 {
		t.Fatalf("expansions in the plan = %d, want 1", expansions)
	}
	if _, err := instance.Run(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	converted, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	// mu-law 0x00 is the most negative code and 0x80 the most positive; the
	// header has to say sixteen-bit PCM once those are expanded.
	want := []byte{0x84, 0x82, 0x7c, 0x7d}
	if !bytes.Contains(converted, want) {
		t.Fatalf("converted payload = %x, want it to carry %x", converted, want)
	}
	if !bytes.Contains(converted, waveFormat(waveShape{channels: 1, bits: 16}, 2)) {
		t.Fatalf("converted header does not declare 16-bit PCM: %x", converted)
	}
}
