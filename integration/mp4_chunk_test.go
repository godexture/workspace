package integration_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/godexture/godec/standard"
)

// TestMP4RemuxRebuildsInterleavedChunkTables covers the geometry every earlier
// MP4 fixture stopped short of: more than one chunk per track, stored
// alternately the way an encoder writes a movie. A single-chunk fixture
// exercises one chunk-offset entry per track, so nothing about rebuilding a
// real stco -- entry order, per-chunk arithmetic, the journal holding more than
// one record -- was reachable end to end.
//
// The assertions here are about the rebuilt tables agreeing with the rebuilt
// payload, which every physical layout has to satisfy.
func TestMP4RemuxRebuildsInterleavedChunkTables(t *testing.T) {
	const chunks, samplesPerChunk = 4, 3
	stored := mp4InterleavedFixture(chunks, samplesPerChunk)
	assertMP4ChunksAlternate(t, stored)

	directory := t.TempDir()
	inputPath := filepath.Join(directory, "interleaved.mp4")
	outputPath := filepath.Join(directory, "interleaved-out.mp4")
	if err := os.WriteFile(inputPath, stored, 0o600); err != nil {
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
	result, runErr := prepared.Run(t.Context())
	if runErr != nil || !result.Succeeded() {
		t.Fatalf("interleaved MP4 remux Run = %#v, %v", result, runErr)
	}
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	assertMP4ChunkTablesTileTheMedia(t, encoded, [][]byte{{0xde, 0xad}, {0xca, 0xfe, 0xba}})
	// The interleave is what a player reads the movie through: a remux that
	// grouped each track's chunks together would still describe the samples
	// correctly while making progressive playback seek across the whole file.
	assertMP4ChunksAlternate(t, encoded)
	if !bytes.Equal(encoded, stored) {
		t.Fatal("preserve-all remux changed an interleaved movie it kept every part of")
	}
	for track := range 2 {
		if got := mp4TrackChunkOffsets(t, encoded, track); len(got) != chunks {
			t.Fatalf("track %d kept %d chunks, want %d", track, len(got), chunks)
		}
		if got := mp4TrackSamplesPerChunk(t, encoded, track); got != samplesPerChunk {
			t.Fatalf("track %d samples per chunk = %d, want %d", track, got, samplesPerChunk)
		}
	}
}

// TestMP4RemuxKeepsAMovieWithExternalByteOffsets covers the movies a preserving
// remux used to refuse outright: those carrying a sidx, iloc or tfra, which
// record byte offsets the sample tables know nothing about. Rebuilding mdat in
// some other order would leave those offsets pointing at bytes that moved, but
// a remux that reproduces the source moves nothing, so the movie round-trips
// instead of being rejected.
func TestMP4RemuxKeepsAMovieWithExternalByteOffsets(t *testing.T) {
	stored := mp4ExternalOffsetFixture(4, 3)
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "indexed.mp4")
	outputPath := filepath.Join(directory, "indexed-out.mp4")
	if err := os.WriteFile(inputPath, stored, 0o600); err != nil {
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
	result, runErr := prepared.Run(t.Context())
	if runErr != nil || !result.Succeeded() {
		t.Fatalf("indexed MP4 remux Run = %#v, %v", result, runErr)
	}
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, stored) {
		t.Fatal("a movie whose external offsets must not move was not reproduced exactly")
	}
	assertMP4ChunkTablesTileTheMedia(t, encoded, [][]byte{{0xde, 0xad}, {0xca, 0xfe, 0xba}})
}

// assertMP4ChunksAlternate fails unless the stored chunks of the two tracks
// really do take turns. Without it an interleaved vector could silently degrade
// into the track-major shape it exists to avoid.
func assertMP4ChunksAlternate(t testing.TB, value []byte) {
	t.Helper()
	first, second := mp4TrackChunkOffsets(t, value, 0), mp4TrackChunkOffsets(t, value, 1)
	if len(first) < 2 || len(first) != len(second) {
		t.Fatalf("stored chunk counts = %d and %d, want at least two each", len(first), len(second))
	}
	for index := range first {
		if first[index] >= second[index] {
			t.Fatalf("stored chunk %d: track 0 at %d is not before track 1 at %d", index, first[index], second[index])
		}
		if index+1 < len(first) && second[index] >= first[index+1] {
			t.Fatalf("stored chunk %d: track 1 at %d is not before the next track 0 chunk at %d", index, second[index], first[index+1])
		}
	}
}

// assertMP4ChunkTablesTileTheMedia checks that every chunk-offset entry
// addresses its own track's payload and that the chunks together cover mdat
// exactly once, with no gap and no overlap. That holds whatever order the
// payload was written in, so it stays the correctness statement when the
// physical layout changes.
func assertMP4ChunkTablesTileTheMedia(t testing.TB, value []byte, payloads [][]byte) {
	t.Helper()
	media := mp4MediaBox(t, value)
	mediaStart := uint64(media.start + 8)
	mediaEnd := mediaStart + uint64(len(media.payload))
	covered := make([]bool, len(media.payload))
	for track, payload := range payloads {
		samples := mp4TrackSamplesPerChunk(t, value, track)
		size := uint64(samples) * uint64(len(payload))
		want := bytes.Repeat(payload, int(samples))
		for index, offset := range mp4TrackChunkOffsets(t, value, track) {
			end := offset + size
			if offset < mediaStart || end > mediaEnd {
				t.Fatalf("track %d chunk %d spans [%d,%d), outside mdat [%d,%d)", track, index, offset, end, mediaStart, mediaEnd)
			}
			if got := value[offset:end]; !bytes.Equal(got, want) {
				t.Fatalf("track %d chunk %d addresses %x, want %x", track, index, got, want)
			}
			for position := offset - mediaStart; position < end-mediaStart; position++ {
				if covered[position] {
					t.Fatalf("track %d chunk %d overlaps an earlier chunk at %d", track, index, mediaStart+position)
				}
				covered[position] = true
			}
		}
	}
	for position, ok := range covered {
		if !ok {
			t.Fatalf("mdat byte %d is not addressed by any chunk", mediaStart+uint64(position))
		}
	}
}

// mp4TrackSampleTable walks to one track's stbl, in the order the tracks appear
// in moov.
func mp4TrackSampleTable(t testing.TB, value []byte, index int) mp4FixtureBoxView {
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
		t.Fatalf("MP4 has %d tracks, wanted track %d", len(tracks), index)
	}
	table := tracks[index]
	for _, step := range []string{"mdia", "minf", "stbl"} {
		child, ok := mp4FixtureChild(table, step)
		if !ok {
			t.Fatalf("track %d has no %s", index, step)
		}
		table = child
	}
	return table
}

// mp4TrackChunkOffsets reads every stco or co64 entry of one track in table
// order.
func mp4TrackChunkOffsets(t testing.TB, value []byte, index int) []uint64 {
	t.Helper()
	table := mp4TrackSampleTable(t, value, index)
	width, entries := 4, mp4FixtureBoxView{}
	if found, ok := mp4FixtureChild(table, "stco"); ok {
		entries = found
	} else if found, ok := mp4FixtureChild(table, "co64"); ok {
		entries, width = found, 8
	} else {
		t.Fatalf("track %d has no chunk-offset table", index)
	}
	if len(entries.payload) < 8 {
		t.Fatalf("track %d chunk-offset table is truncated", index)
	}
	count := int(binary.BigEndian.Uint32(entries.payload[4:8]))
	if len(entries.payload) != 8+count*width {
		t.Fatalf("track %d chunk-offset table holds %d bytes for %d entries", index, len(entries.payload), count)
	}
	result := make([]uint64, count)
	for entry := range count {
		row := entries.payload[8+entry*width:]
		if width == 4 {
			result[entry] = uint64(binary.BigEndian.Uint32(row[:4]))
			continue
		}
		result[entry] = binary.BigEndian.Uint64(row[:8])
	}
	return result
}

// mp4TrackSamplesPerChunk reads the samples_per_chunk of a track described by a
// single stsc run.
func mp4TrackSamplesPerChunk(t testing.TB, value []byte, index int) uint32 {
	t.Helper()
	table := mp4TrackSampleTable(t, value, index)
	entries, ok := mp4FixtureChild(table, "stsc")
	if !ok || len(entries.payload) != 8+12 {
		t.Fatalf("track %d stsc is absent or holds more than one run", index)
	}
	return binary.BigEndian.Uint32(entries.payload[12:16])
}
