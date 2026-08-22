package integration_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/godexture/godec/standard"
)

// TestMP4RemuxWritesSamplesInEmitOrder is the M7-C11 physical-order vector. In a
// direct single synchronous execution island the routed reader's emit calls are
// the only thing deciding where a sample lands, so a source whose mdat is not in
// track order comes out in track order with its chunk offsets patched to follow.
// The bytes move; the track order, sample payloads and per-track tables do not.
func TestMP4RemuxWritesSamplesInEmitOrder(t *testing.T) {
	stored := mp4StoredOutOfOrderFixture()
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "stored-out-of-order.mp4")
	outputPath := filepath.Join(directory, "emitted.mp4")
	if err := os.WriteFile(inputPath, stored, 0o600); err != nil {
		t.Fatal(err)
	}
	// The fixture must actually disagree with route order, or the vector proves
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

	if bytes.Equal(encoded, stored) {
		t.Fatal("remux reproduced the stored sample order instead of the emit order")
	}
	if got := mp4MediaPayload(t, encoded); !bytes.Equal(got, []byte{0xde, 0xad, 0xca, 0xfe, 0xba}) {
		t.Fatalf("remuxed mdat payload = %x, want route order", got)
	}
	assertMP4FixtureSemantics(t, encoded)

	// Each rebuilt chunk offset must address that track's own bytes, otherwise
	// the moved payload would be silently mislabeled rather than remuxed.
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

// mp4TrackChunkOffset reads the first stco or co64 entry of one track, in the
// order the tracks appear in moov.
func mp4TrackChunkOffset(t testing.TB, value []byte, index int) uint64 {
	t.Helper()
	var tracks []mp4FixtureBoxView
	for _, box := range mp4FixtureTopLevel(value) {
		if box.typeID != "moov" {
			continue
		}
		for _, child := range mp4FixtureChildren(box.payload, 0, len(box.payload)) {
			if child.typeID == "trak" {
				tracks = append(tracks, child)
			}
		}
	}
	if index < 0 || index >= len(tracks) {
		t.Fatalf("MP4 output has %d tracks, wanted track %d", len(tracks), index)
	}
	table := tracks[index]
	for _, step := range []string{"mdia", "minf", "stbl"} {
		child, ok := mp4FixtureChild(table, step)
		if !ok {
			t.Fatalf("track %d has no %s", index, step)
		}
		table = child
	}
	if entries, ok := mp4FixtureChild(table, "stco"); ok {
		if len(entries.payload) < 12 {
			t.Fatalf("track %d stco is truncated", index)
		}
		return uint64(binary.BigEndian.Uint32(entries.payload[8:12]))
	}
	entries, ok := mp4FixtureChild(table, "co64")
	if !ok || len(entries.payload) < 16 {
		t.Fatalf("track %d has no readable chunk-offset table", index)
	}
	return binary.BigEndian.Uint64(entries.payload[8:16])
}
