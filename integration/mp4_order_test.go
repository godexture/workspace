package integration_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/godexture/godec/standard"
)

// TestMP4RemuxKeepsTheStoredSampleOrder is the M7-C11 physical-order vector.
// The reader emits samples in the order the source stored them, and a direct
// single synchronous execution island is what carries that order through to the
// muxer unchanged, so the rebuilt mdat holds the same bytes in the same places.
// Storage order is a property of the movie a player reads; a remux that
// preserves everything else has no reason to be the one thing that rearranges
// it.
func TestMP4RemuxKeepsTheStoredSampleOrder(t *testing.T) {
	stored := mp4StoredOutOfOrderFixture()
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "stored-out-of-order.mp4")
	outputPath := filepath.Join(directory, "emitted.mp4")
	if err := os.WriteFile(inputPath, stored, 0o600); err != nil {
		t.Fatal(err)
	}
	// The fixture must actually disagree with track order, or the vector proves
	// nothing about which one the output follows.
	if got := mp4MediaPayload(t, stored); !bytes.Equal(got, []byte{0xca, 0xfe, 0xba, 0xde, 0xad}) {
		t.Fatalf("stored mdat payload = %x, want the second track first", got)
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
	result, runErr := prepared.Run(t.Context())
	if runErr != nil || !result.Succeeded() {
		t.Fatalf("out-of-order MP4 remux Run = %#v, %v", result, runErr)
	}
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}

	if got := mp4MediaPayload(t, encoded); !bytes.Equal(got, []byte{0xca, 0xfe, 0xba, 0xde, 0xad}) {
		t.Fatalf("remuxed mdat payload = %x, want the stored order", got)
	}
	// Keeping every track means keeping every box, so preserving the payload
	// order leaves nothing left to differ.
	if !bytes.Equal(encoded, stored) {
		t.Fatal("preserve-all remux changed a movie it kept every part of")
	}
	assertMP4FixtureSemantics(t, encoded)

	// Each rebuilt chunk offset must address that track's own bytes, otherwise
	// the payload would be silently mislabeled rather than remuxed.
	base := mp4MediaPayloadOffset(t, encoded)
	for index, want := range [][]byte{{0xde, 0xad}, {0xca, 0xfe, 0xba}} {
		offset := mp4TrackChunkOffset(t, encoded, index)
		if offset < base || offset+uint64(len(want)) > uint64(len(encoded)) {
			t.Fatalf("track %d chunk offset %d lies outside the output", index, offset)
		}
		if got := encoded[offset : offset+uint64(len(want))]; !bytes.Equal(got, want) {
			t.Fatalf("track %d chunk offset %d addresses %x, want %x", index, offset, got, want)
		}
	}
}

func mp4MediaBox(t testing.TB, value []byte) mp4FixtureBoxView {
	t.Helper()
	for _, box := range mp4FixtureTopLevel(value) {
		if box.typeID == "mdat" {
			return box
		}
	}
	t.Fatal("MP4 output has no mdat")
	return mp4FixtureBoxView{}
}

func mp4MediaPayload(t testing.TB, value []byte) []byte {
	t.Helper()
	return mp4MediaBox(t, value).payload
}

func mp4MediaPayloadOffset(t testing.TB, value []byte) uint64 {
	t.Helper()
	return uint64(mp4MediaBox(t, value).start + 8)
}

// mp4TrackChunkOffset reads the first chunk-offset entry of one track.
func mp4TrackChunkOffset(t testing.TB, value []byte, index int) uint64 {
	t.Helper()
	offsets := mp4TrackChunkOffsets(t, value, index)
	if len(offsets) == 0 {
		t.Fatalf("track %d has no chunk-offset entries", index)
	}
	return offsets[0]
}
