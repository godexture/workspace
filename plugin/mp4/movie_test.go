package mp4

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/godexture/godec/resource"
)

func TestParseMovieBuildsBoundedTracksAndLazySamples(t *testing.T) {
	data := twoTrackMovie(false, "isom", "iso2")
	parsed, err := parseMovie(context.Background(), memoryRandom(data), uint64(len(data)), 1<<20, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.fileType.major != boxTypeOf("isom") || parsed.sourceEnd != uint64(len(data)) || parsed.media.typeID != typeMDAT || len(parsed.tracks) != 2 {
		t.Fatalf("movie = %#v", parsed)
	}
	first, second := parsed.tracks[0], parsed.tracks[1]
	if first.id != 1 || first.timeScale != 48000 || first.handler != boxTypeOf("soun") || first.codec != boxTypeOf("mp4a") || first.sampleCount != 1 || first.maxSampleSize != 2 {
		t.Fatalf("first track = %#v", first)
	}
	if second.id != 2 || second.timeScale != 1000 || second.handler != boxTypeOf("vide") || second.codec != boxTypeOf("avc1") || second.sampleCount != 1 || second.maxSampleSize != 3 {
		t.Fatalf("second track = %#v", second)
	}
	for _, value := range parsed.tracks {
		if value.trak.typeID != typeTRAK || value.tables.timing.typeID != typeSTTS || value.tables.layout.typeID != typeSTSC || value.tables.sizes.typeID != typeSTSZ && value.tables.sizes.typeID != typeSTZ2 {
			t.Fatalf("track table ranges = %#v", value)
		}
		assertNoRetainedSampleState(t, value)
	}

	cursor, err := newSampleCursor(context.Background(), memoryRandom(data), second)
	if err != nil {
		t.Fatal(err)
	}
	item, more, err := cursor.next(context.Background())
	if err != nil || !more {
		t.Fatalf("cursor.next() = %#v, %t, %v", item, more, err)
	}
	if item.size != 3 || item.duration != 40 || item.dts != 0 || item.pts != -2 || !item.sync || item.sequence != 1 {
		t.Fatalf("sample = %#v", item)
	}
	if _, more, err := cursor.next(context.Background()); err != nil || more {
		t.Fatalf("cursor EOF = more=%t err=%v", more, err)
	}
}

func TestParseMovieRejectsMultipleMdat(t *testing.T) {
	data := append(twoTrackMovie(false, "isom", "iso2"), fixtureBox("mdat", nil)...)
	_, err := parseMovie(context.Background(), memoryRandom(data), uint64(len(data)), 1<<20, 1<<20)
	if !errors.Is(err, errUnsupportedMovie) || !strings.Contains(err.Error(), "multiple mdat") {
		t.Fatalf("parseMovie() error = %v, want multiple-mdat rejection", err)
	}
}

func TestInspectReadLimitStopsBeforeTablePageRead(t *testing.T) {
	data := manyTimingMovie(1_024)
	reader := &recordingRandom{data: data}
	_, err := parseMovie(context.Background(), reader, uint64(len(data)), resource.Bytes(tablePageBytes), 1<<20)
	if !errors.Is(err, errUnsupportedMovie) {
		t.Fatalf("parseMovie() error = %v, want read-limit rejection", err)
	}
	if reader.largestRead >= tablePageBytes {
		t.Fatalf("inspect read %d-byte table page after budget exhaustion", reader.largestRead)
	}
}

func TestSampleCursorReconstructsMixedTables(t *testing.T) {
	data, want := mixedTableMovie(false)
	parsed, err := parseMovie(context.Background(), memoryRandom(data), uint64(len(data)), 1<<20, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := newSampleCursor(context.Background(), memoryRandom(data), parsed.tracks[0])
	if err != nil {
		t.Fatal(err)
	}
	for index, expected := range want {
		got, more, err := cursor.next(context.Background())
		if err != nil || !more || got != expected {
			t.Fatalf("sample %d = %#v, %t, %v; want %#v", index+1, got, more, err, expected)
		}
	}
	if _, more, err := cursor.next(context.Background()); err != nil || more {
		t.Fatalf("cursor EOF = more=%t err=%v", more, err)
	}
}

func TestParseMovieAcceptsMoovAfterMdatAndCO64(t *testing.T) {
	data := twoTrackMovie(true, "isom", "iso2")
	parsed, err := parseMovie(context.Background(), memoryRandom(data), uint64(len(data)), 1<<20, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.tracks[1].tables.largeOffsets || parsed.tracks[1].tables.offsets.typeID != typeCO64 {
		t.Fatalf("co64 range = %#v", parsed.tracks[1].tables)
	}
}

func TestInspectionRetainedStateDoesNotScaleWithSamples(t *testing.T) {
	small := parseFixtureMovie(t, manySampleMovie(1_000))
	large := parseFixtureMovie(t, manySampleMovie(1_000_000))
	if got, want := retainedMovieBytes(small), retainedMovieBytes(large); got != want {
		t.Fatalf("retained model bytes = %d for 1k, %d for 1m samples", got, want)
	}
	if small.tracks[0].sampleCount != 1_000 || large.tracks[0].sampleCount != 1_000_000 {
		t.Fatalf("sample summaries = %d, %d", small.tracks[0].sampleCount, large.tracks[0].sampleCount)
	}
}

func TestInspectionKeepsOpaqueBoxesAsSourceRanges(t *testing.T) {
	track := fixtureTrack{id: 1, timeScale: 1_000, handler: "vide", entryType: "avc1", size: 1, sttsExtra: []fixtureTiming{{count: 1, duration: 1}}}
	plain := parseFixtureMovie(t, fixtureMovie(false, "isom", []string{"iso2"}, []fixtureTrack{track}, nil, nil))
	data := fixtureMovie(false, "isom", []string{"iso2"}, []fixtureTrack{track}, [][]byte{fixtureBox("free", make([]byte, 2<<20))}, nil)
	reader := &recordingRandom{data: data}
	opaque, err := parseMovie(context.Background(), reader, uint64(len(data)), 1<<20, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if retainedMovieBytes(plain) != retainedMovieBytes(opaque) {
		t.Fatalf("opaque box changed retained model size: %d -> %d", retainedMovieBytes(plain), retainedMovieBytes(opaque))
	}
	if reader.largestRead > 112 {
		t.Fatalf("inspection read %d opaque bytes, want only fixed fields", reader.largestRead)
	}
}

func TestMemoryLimitAccountsForRetainedTrackModel(t *testing.T) {
	data := twoTrackMovie(false, "isom", "iso2")
	_, err := parseMovie(context.Background(), memoryRandom(data), uint64(len(data)), 1<<20, resource.Bytes(trackBudgetBytes-1))
	if !errors.Is(err, errUnsupportedMovie) {
		t.Fatalf("parseMovie() error = %v, want retained-model limit", err)
	}
}

func TestSampleCursorHotPathDoesNotAllocate(t *testing.T) {
	data, _ := mixedTableMovie(false)
	parsed := parseFixtureMovie(t, data)
	template, err := newSampleCursor(context.Background(), memoryRandom(data), parsed.tracks[0])
	if err != nil {
		t.Fatal(err)
	}
	cursors := make([]sampleCursor, 256)
	for index := range cursors {
		cursors[index] = template
	}
	run := 0
	if allocations := testing.AllocsPerRun(100, func() {
		cursor := &cursors[run]
		run++
		for {
			_, more, err := cursor.next(context.Background())
			if err != nil || !more {
				return
			}
		}
	}); allocations != 0 {
		t.Fatalf("cursor steady-state allocations = %f, want 0", allocations)
	}
}

func TestSampleCursorCancellationAndTruncation(t *testing.T) {
	data, _ := mixedTableMovie(false)
	parsed := parseFixtureMovie(t, data)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := newSampleCursor(ctx, memoryRandom(data), parsed.tracks[0]); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled cursor error = %v", err)
	}
	if _, err := newSampleCursor(context.Background(), memoryRandom(data[:8]), parsed.tracks[0]); !errors.Is(err, errTruncatedMovie) {
		t.Fatalf("truncated cursor error = %v", err)
	}
}

func TestParseMovieBrandPolicy(t *testing.T) {
	for _, test := range []struct {
		major, compatible string
		want              error
	}{
		{major: "M4A ", compatible: "isom"},
		{major: "xfoo", compatible: "isom"},
		{major: "heic", compatible: "mif1", want: errUnsupportedMovie},
	} {
		data := twoTrackMovie(false, test.major, test.compatible)
		_, err := parseMovie(context.Background(), memoryRandom(data), uint64(len(data)), 1<<20, 1<<20)
		if !errors.Is(err, test.want) {
			t.Fatalf("brand %q/%q error = %v, want %v", test.major, test.compatible, err, test.want)
		}
	}
}

func parseFixtureMovie(t testing.TB, data []byte) movie {
	t.Helper()
	parsed, err := parseMovie(context.Background(), memoryRandom(data), uint64(len(data)), 1<<20, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func retainedMovieBytes(value movie) uintptr {
	return unsafe.Sizeof(value) + uintptr(cap(value.tracks))*unsafe.Sizeof(track{})
}

func assertNoRetainedSampleState(t testing.TB, value track) {
	t.Helper()
	typeOf := reflect.TypeOf(value)
	for index := 0; index < typeOf.NumField(); index++ {
		if typeOf.Field(index).Type.Kind() == reflect.Slice {
			t.Fatalf("track retains a slice in %s", typeOf.Field(index).Name)
		}
	}
}
