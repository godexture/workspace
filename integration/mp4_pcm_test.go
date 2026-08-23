package integration_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/godexture/godec/plugin/pcm/linear"
	"github.com/godexture/godec/standard"
)

// TestMP4PCMBindsOnlyWhenTheOutputNeedsIt covers M7-C06 from both sides. A
// little-endian sowt track already matches the WAVE data chunk, so its packets
// are copied; a big-endian twos track cannot be copied into WAVE, so the planner
// reaches for the PCM binding and the samples arrive byte-swapped.
func TestMP4PCMBindsOnlyWhenTheOutputNeedsIt(t *testing.T) {
	samples := []int16{0, 1, -1, 32767, -32768, 1234, -1234, 4660}
	little := make([]byte, len(samples)*2)
	big := make([]byte, len(samples)*2)
	for index, value := range samples {
		binary.LittleEndian.PutUint16(little[index*2:], uint16(value))
		binary.BigEndian.PutUint16(big[index*2:], uint16(value))
	}

	for _, testCase := range []struct {
		name    string
		entry   string
		payload []byte
		decoded bool
	}{
		{name: "little endian copies", entry: "sowt", payload: little},
		{name: "big endian decodes", entry: "twos", payload: big, decoded: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			directory := t.TempDir()
			inputPath := filepath.Join(directory, "input.mp4")
			outputPath := filepath.Join(directory, "output.wav")
			if err := os.WriteFile(inputPath, mp4PCMFixture(testCase.entry, testCase.payload), 0o600); err != nil {
				t.Fatal(err)
			}
			request, err := standard.NewFileJob(inputPath, outputPath)
			if err != nil {
				t.Fatal(err)
			}
			instance, err := standard.NewHost()
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := instance.Prepare(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			var decoded bool
			var components []string
			for _, node := range prepared.Plan().Nodes() {
				components = append(components, node.Component)
				if node.Component == linear.DecoderIdentity(sample.S16).String() {
					decoded = true
				}
			}
			if decoded != testCase.decoded {
				t.Fatalf("plan decodes PCM = %t, want %t: %v", decoded, testCase.decoded, components)
			}
			result, runErr := prepared.Run(t.Context())
			if runErr != nil || !result.Succeeded() {
				t.Fatalf("MP4 to WAVE Run = %#v, %v", result, runErr)
			}
			if err := prepared.Close(); err != nil {
				t.Fatal(err)
			}
			outputBytes, err := os.ReadFile(outputPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasSuffix(outputBytes, little) {
				t.Fatalf("WAVE data chunk = %x, want %x", outputBytes, little)
			}
		})
	}
}

// TestMP4EditedTrackIsCopiedButNeverDecoded keeps an edit list from being lost
// in silence. An edts maps media time onto the presentation timeline, so a
// decoder that only receives samples would produce the unedited media. Copying
// the track carries the edts along, so an MP4 remux still succeeds; converting
// to WAVE would have to drop it, and fails while planning instead.
func TestMP4EditedTrackIsCopiedButNeverDecoded(t *testing.T) {
	samples := make([]byte, 16)
	for index := range samples {
		samples[index] = byte(index)
	}
	edited := mp4EditedPCMFixture(samples)
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "edited.mp4")
	if err := os.WriteFile(inputPath, edited, 0o600); err != nil {
		t.Fatal(err)
	}
	instance, err := standard.NewHost()
	if err != nil {
		t.Fatal(err)
	}

	remuxPath := filepath.Join(directory, "edited-out.mp4")
	request, err := standard.NewFileJob(inputPath, remuxPath)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := instance.Prepare(t.Context(), request)
	if err != nil {
		t.Fatalf("edited track remux Prepare: %v", err)
	}
	result, runErr := prepared.Run(t.Context())
	if runErr != nil || !result.Succeeded() {
		t.Fatalf("edited track remux Run = %#v, %v", result, runErr)
	}
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(remuxPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, edited) {
		t.Fatal("remux changed a movie whose only extra box is an edit list")
	}

	convert, err := standard.NewFileJob(inputPath, filepath.Join(directory, "edited.wav"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := instance.Plan(t.Context(), convert); err == nil {
		t.Fatal("an edited track was planned for decoding, which would drop its edit list")
	}
}
