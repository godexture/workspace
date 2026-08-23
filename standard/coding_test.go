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
