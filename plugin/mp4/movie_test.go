package mp4

import (
	"bytes"
	"context"
	"errors"
	"math"
	"testing"
	"unsafe"
)

func TestParseMovieExpandsUnfragmentedTracks(t *testing.T) {
	data := twoTrackMovie(false, "isom", "iso2")
	parsed, err := parseMovie(context.Background(), memoryRandom(data), uint64(len(data)), 1<<20, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.fileType.major != boxTypeOf("isom") || len(parsed.media) != 1 || len(parsed.tracks) != 2 {
		t.Fatalf("movie = %#v", parsed)
	}
	first, second := parsed.tracks[0], parsed.tracks[1]
	if first.id != 1 || first.timeScale != 48000 || first.handler != boxTypeOf("soun") || len(first.samples) != 1 {
		t.Fatalf("first track = %#v", first)
	}
	if first.samples[0].size != 2 || first.samples[0].duration != 1024 || first.samples[0].dts != 0 || first.samples[0].pts != 0 || !first.samples[0].sync {
		t.Fatalf("first sample = %#v", first.samples[0])
	}
	if second.id != 2 || second.timeScale != 1000 || second.handler != boxTypeOf("vide") || len(second.samples) != 1 {
		t.Fatalf("second track = %#v", second)
	}
	if second.samples[0].size != 3 || second.samples[0].duration != 40 || second.samples[0].dts != 0 || second.samples[0].pts != -2 || !second.samples[0].sync {
		t.Fatalf("second sample = %#v", second.samples[0])
	}
	if !bytes.Equal(first.descriptions[0].raw[4:8], []byte("mp4a")) || !bytes.Equal(second.descriptions[0].raw[4:8], []byte("avc1")) {
		t.Fatalf("sample descriptions = %#v %#v", first.descriptions, second.descriptions)
	}
	for _, value := range parsed.tracks {
		if value.timing != nil || value.composition != nil || value.chunkLayout != nil || value.chunkOffsets != nil || value.sampleSizes != nil || value.syncSamples != nil || value.hasSyncSample {
			t.Fatalf("parse-only table state survives expansion: %#v", value)
		}
		if len(value.trackHeader) == 0 || len(value.mediaHeader) == 0 || len(value.handlerHeader) == 0 || len(value.dataInfo) == 0 || len(value.mediaInfo) == 0 {
			t.Fatalf("track provenance is incomplete: %#v", value)
		}
	}
}

func TestParseMovieAcceptsMoovAfterMdat(t *testing.T) {
	data := twoTrackMovie(true, "isom", "iso2")
	parsed, err := parseMovie(context.Background(), memoryRandom(data), uint64(len(data)), 1<<20, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.top) != 3 || parsed.top[1].typeID != typeMDAT || parsed.top[2].typeID != typeMOOV || len(parsed.tracks[1].samples) != 1 {
		t.Fatalf("movie order = %#v", parsed.top)
	}
}

func TestParseMoviePreservesDirectRawAnchorOrder(t *testing.T) {
	edts := fixtureContainer("edts", fixtureBox("elst", fixtureFullBox(0, 0, fixtureU32(0))))
	first := fixtureTrack{
		id:            1,
		timeScale:     48000,
		handler:       "soun",
		entryType:     "mp4a",
		size:          2,
		sttsExtra:     []fixtureTiming{{count: 1, duration: 1}},
		directBefore:  [][]byte{fixtureBox("free", []byte{1})},
		directBetween: [][]byte{fixtureBox("skip", []byte{2})},
		directAfter:   [][]byte{fixtureBox("wide", []byte{3}), edts},
	}
	second := fixtureTrack{id: 2, timeScale: 1000, handler: "vide", entryType: "avc1", size: 3, compact: true, largeOffset: true, sttsExtra: []fixtureTiming{{count: 1, duration: 1}}}
	data := fixtureMovie(false, "isom", []string{"iso2"}, []fixtureTrack{first, second}, [][]byte{fixtureBox("free", []byte{9})}, [][]byte{fixtureBox("udta", []byte{8})})
	parsed, err := parseMovie(context.Background(), memoryRandom(data), uint64(len(data)), 1<<20, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	assertAnchorTypes(t, parsed.top, "ftyp", "free", "moov", "mdat")
	assertAnchorTypes(t, parsed.moov, "mvhd", "udta", "trak", "trak")
	assertAnchorTypes(t, parsed.tracks[0].anchors, "free", "tkhd", "skip", "mdia", "wide", "edts")
	for _, index := range []int{0, 2, 4, 5} {
		if len(parsed.tracks[0].anchors[index].raw) == 0 {
			t.Fatalf("raw trak anchor %d was not preserved", index)
		}
	}
	if parsed.tracks[0].anchors[1].raw != nil || parsed.tracks[0].anchors[3].raw != nil {
		t.Fatalf("known trak anchors unexpectedly carry raw bytes: %#v", parsed.tracks[0].anchors)
	}
	if len(parsed.moov[0].raw) == 0 || len(parsed.moov[1].raw) == 0 {
		t.Fatalf("mvhd or raw moov anchor was discarded: %#v", parsed.moov)
	}
	if !bytes.Equal(parsed.tracks[0].anchors[5].raw, edts) {
		t.Fatalf("edts raw anchor = %x, want %x", parsed.tracks[0].anchors[5].raw, edts)
	}
}

func TestParseMovieFreezesRawProvenance(t *testing.T) {
	data := twoTrackMovie(false, "isom", "iso2")
	parsed, err := parseMovie(context.Background(), memoryRandom(data), uint64(len(data)), 1<<20, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), parsed.tracks[0].trackHeader...)
	for index := range data {
		data[index] = 0
	}
	if !bytes.Equal(parsed.tracks[0].trackHeader, before) || len(parsed.tracks[0].descriptions[0].raw) == 0 {
		t.Fatal("movie provenance aliases input bytes")
	}
}

func TestParseMovieBrandPolicy(t *testing.T) {
	t.Run("known audio brand", func(t *testing.T) {
		data := twoTrackMovie(false, "M4A ", "isom")
		if _, err := parseMovie(context.Background(), memoryRandom(data), uint64(len(data)), 1<<20, 1<<20); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("compatible MP4 brand", func(t *testing.T) {
		data := twoTrackMovie(false, "xfoo", "isom")
		if _, err := parseMovie(context.Background(), memoryRandom(data), uint64(len(data)), 1<<20, 1<<20); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("unknown only", func(t *testing.T) {
		data := twoTrackMovie(false, "heic", "mif1")
		_, err := parseMovie(context.Background(), memoryRandom(data), uint64(len(data)), 1<<20, 1<<20)
		if !errors.Is(err, errUnsupportedMovie) {
			t.Fatalf("parseMovie() error = %v, want unsupported", err)
		}
	})
}

func TestParseMovieRejectsBudgetBeforeRawRead(t *testing.T) {
	data := fixtureBox("free", make([]byte, 64))
	value := collectTopLevel(t, data)[0]
	reader := &recordingRandom{data: data}
	_, err := preserveRaw(context.Background(), reader, value, &movieBudget{readLimit: math.MaxUint64, remaining: anchorBudgetBytes}, "test raw")
	if !errors.Is(err, errUnsupportedMovie) {
		t.Fatalf("preserveRaw() error = %v, want budget failure", err)
	}
	if reader.largestRead != 0 {
		t.Fatalf("budget failure read %d bytes before rejecting raw allocation", reader.largestRead)
	}
}

func TestMovieBudgetFitsOneHourAVSampleModel(t *testing.T) {
	const audioSamples = uint64(3600 * 48000 / 1024)
	const videoSamples = uint64(3600 * 30)
	budget := movieBudget{remaining: 16 << 20}
	if err := budget.reserveRecords(audioSamples+videoSamples, sampleBudgetBytes, "one-hour A/V samples"); err != nil {
		t.Fatalf("one-hour A/V sample model does not fit: %v", err)
	}
}

func TestSampleBudgetCoversModelSize(t *testing.T) {
	if size := unsafe.Sizeof(sample{}); size > uintptr(sampleBudgetBytes) {
		t.Fatalf("sample size = %d, budget = %d", size, sampleBudgetBytes)
	}
}

func TestParseMovieHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := parseMovie(ctx, memoryRandom(twoTrackMovie(false, "isom", "iso2")), 1, 1<<20, 1<<20)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("parseMovie() error = %v, want canceled", err)
	}
}

func assertAnchorTypes(t *testing.T, values []anchor, want ...string) {
	t.Helper()
	if len(values) != len(want) {
		t.Fatalf("anchor count = %d, want %d: %#v", len(values), len(want), values)
	}
	for index, value := range values {
		if got := string(value.typeID[:]); got != want[index] {
			t.Fatalf("anchor %d = %q, want %q", index, got, want[index])
		}
	}
}
