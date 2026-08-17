package mp4

import (
	"context"
	"errors"
	"math"
	"testing"
)

func TestParseMovieRejectsMovieStructure(t *testing.T) {
	fixture := fixtureTrack{id: 1, timeScale: 1000, handler: "vide", entryType: "avc1", size: 1, sttsExtra: []fixtureTiming{{count: 1, duration: 1}}}
	cases := []struct {
		name string
		moov []byte
		want error
	}{
		{
			name: "missing mvhd",
			moov: fixtureContainer("moov", fixtureTrackBox(fixture)),
			want: errMalformedMovie,
		},
		{
			name: "duplicate mvhd",
			moov: fixtureContainer("moov", fixtureMVHD(), fixtureMVHD(), fixtureTrackBox(fixture)),
			want: errMalformedMovie,
		},
		{
			name: "fragment",
			moov: fixtureContainer("moov", fixtureMVHD(), fixtureBox("mvex", nil), fixtureTrackBox(fixture)),
			want: errUnsupportedMovie,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			data := append(fixtureFileType("isom", "iso2"), test.moov...)
			data = append(data, fixtureBox("mdat", []byte{1})...)
			_, err := parseMovie(context.Background(), memoryRandom(data), uint64(len(data)), 1<<20, 1<<20)
			if !errors.Is(err, test.want) {
				t.Fatalf("parseMovie() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestParseMovieRejectsTableInconsistency(t *testing.T) {
	cases := []struct {
		name  string
		track fixtureTrack
		want  error
	}{
		{
			name:  "sample count mismatch",
			track: fixtureTrack{id: 1, timeScale: 1000, handler: "vide", entryType: "avc1", size: 1, sttsExtra: []fixtureTiming{{count: 2, duration: 1}}},
			want:  errMalformedMovie,
		},
		{
			name:  "description index",
			track: fixtureTrack{id: 1, timeScale: 1000, handler: "vide", entryType: "avc1", size: 1, sttsExtra: []fixtureTiming{{count: 1, duration: 1}}, stsc: []fixtureChunk{{first: 1, samples: 1, description: 2}}},
			want:  errMalformedMovie,
		},
		{
			name:  "sample outside mdat",
			track: fixtureTrack{id: 1, timeScale: 1000, handler: "vide", entryType: "avc1", size: 1, offsetDelta: 100, sttsExtra: []fixtureTiming{{count: 1, duration: 1}}},
			want:  errMalformedMovie,
		},
		{
			name:  "unknown stbl child",
			track: fixtureTrack{id: 1, timeScale: 1000, handler: "vide", entryType: "avc1", size: 1, sttsExtra: []fixtureTiming{{count: 1, duration: 1}}, stblExtra: [][]byte{fixtureBox("free", nil)}},
			want:  errUnsupportedMovie,
		},
		{
			name:  "encrypted entry",
			track: fixtureTrack{id: 1, timeScale: 1000, handler: "vide", entryType: "encv", size: 1, sttsExtra: []fixtureTiming{{count: 1, duration: 1}}},
			want:  errUnsupportedMovie,
		},
		{
			name:  "elst outside edts",
			track: fixtureTrack{id: 1, timeScale: 1000, handler: "vide", entryType: "avc1", size: 1, sttsExtra: []fixtureTiming{{count: 1, duration: 1}}, directBefore: [][]byte{fixtureBox("elst", nil)}},
			want:  errUnsupportedMovie,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			data := fixtureMovie(false, "isom", []string{"iso2"}, []fixtureTrack{test.track}, nil, nil)
			_, err := parseMovie(context.Background(), memoryRandom(data), uint64(len(data)), 1<<20, 1<<20)
			if !errors.Is(err, test.want) {
				t.Fatalf("parseMovie() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestParseCompositionV0AndCheckedPTS(t *testing.T) {
	fixture := fixtureTrack{
		id:          1,
		timeScale:   1000,
		handler:     "vide",
		entryType:   "avc1",
		size:        1,
		composition: &fixtureComposition{version: 0, offset: 2},
		sttsExtra:   []fixtureTiming{{count: 1, duration: 1}},
	}
	data := fixtureMovie(false, "isom", []string{"iso2"}, []fixtureTrack{fixture}, nil, nil)
	parsed, err := parseMovie(context.Background(), memoryRandom(data), uint64(len(data)), 1<<20, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.tracks[0].samples[0].pts; got != 2 {
		t.Fatalf("ctts v0 PTS = %d, want 2", got)
	}

	if _, ok := addCompositionOffset(math.MaxInt64, 1); ok {
		t.Fatal("addCompositionOffset accepted an overflowing PTS")
	}
}

func TestParseDataReferencesRejectsExternalAndTruncatedData(t *testing.T) {
	external := fixtureBox("dref", append(fixtureFullBox(0, 0, fixtureU32(1)), fixtureBox("url ", fixtureFullBox(0, 0, []byte("https://example.invalid")))...))
	boxes := collectTopLevel(t, external)
	_, err := parseDataReferences(context.Background(), memoryRandom(external), uint64(len(external)), boxes[0])
	if !errors.Is(err, errUnsupportedMovie) {
		t.Fatalf("parseDataReferences() error = %v, want external-reference rejection", err)
	}

	data := twoTrackMovie(false, "isom", "iso2")
	_, err = parseMovie(context.Background(), memoryRandom(data[:4]), uint64(len(data)), 1<<20, 1<<20)
	if !errors.Is(err, errTruncatedMovie) {
		t.Fatalf("parseMovie() error = %v, want truncated", err)
	}
}

func TestHeaderFieldMinima(t *testing.T) {
	mvhd := make([]byte, 112)
	mvhd[0] = 1
	copy(mvhd[20:24], fixtureU32(1000))
	if err := parseMovieHeader(mvhd); err != nil {
		t.Fatal(err)
	}
	if err := parseMovieHeader(mvhd[:111]); !errors.Is(err, errMalformedMovie) {
		t.Fatalf("parseMovieHeader() error = %v, want malformed", err)
	}

	tkhd := make([]byte, 96)
	tkhd[0] = 1
	copy(tkhd[20:24], fixtureU32(1))
	if id, err := parseTrackID(tkhd); err != nil || id != 1 {
		t.Fatalf("parseTrackID() = %d, %v", id, err)
	}
	if _, err := parseTrackID(tkhd[:95]); !errors.Is(err, errMalformedMovie) {
		t.Fatalf("parseTrackID() error = %v, want malformed", err)
	}

	mdhd := make([]byte, 36)
	mdhd[0] = 1
	copy(mdhd[20:24], fixtureU32(1000))
	if timeScale, err := parseTimeScale(mdhd); err != nil || timeScale != 1000 {
		t.Fatalf("parseTimeScale() = %d, %v", timeScale, err)
	}
	if _, err := parseTimeScale(mdhd[:35]); !errors.Is(err, errMalformedMovie) {
		t.Fatalf("parseTimeScale() error = %v, want malformed", err)
	}

	hdlr := make([]byte, 24)
	copy(hdlr[8:12], "vide")
	if handler, err := parseHandler(hdlr); err != nil || handler != boxTypeOf("vide") {
		t.Fatalf("parseHandler() = %q, %v", handler, err)
	}
	if _, err := parseHandler(hdlr[:23]); !errors.Is(err, errMalformedMovie) {
		t.Fatalf("parseHandler() error = %v, want malformed", err)
	}
}

func TestTableRunsRejectZeroCounts(t *testing.T) {
	stts := fixtureSTTS([]fixtureTiming{{count: 0, duration: 1}})
	sttsBox := collectTopLevel(t, stts)[0]
	if _, err := parseTimingTable(context.Background(), memoryRandom(stts), sttsBox, &movieBudget{readLimit: math.MaxUint64, remaining: math.MaxUint64}); !errors.Is(err, errMalformedMovie) {
		t.Fatalf("parseTimingTable() error = %v, want malformed", err)
	}

	cttsPayload := fixtureFullBox(0, 0, fixtureU32(1))
	cttsPayload = append(cttsPayload, fixtureU32(0)...)
	cttsPayload = append(cttsPayload, fixtureU32(0)...)
	ctts := fixtureBox("ctts", cttsPayload)
	cttsBox := collectTopLevel(t, ctts)[0]
	if _, err := parseCompositionTable(context.Background(), memoryRandom(ctts), cttsBox, &movieBudget{readLimit: math.MaxUint64, remaining: math.MaxUint64}); !errors.Is(err, errMalformedMovie) {
		t.Fatalf("parseCompositionTable() error = %v, want malformed", err)
	}
}

func TestParserRejectsLargeAllocationsBeforeReadingOrMaking(t *testing.T) {
	reader := &recordingRandom{data: make([]byte, 16)}
	budget := movieBudget{readLimit: 32, remaining: 32}
	largePayload := box{payloadOffset: 0, payloadSize: 64}
	if _, err := readBoxData(context.Background(), reader, largePayload, &budget, "large table"); !errors.Is(err, errUnsupportedMovie) {
		t.Fatalf("readBoxData() error = %v, want allocation rejection", err)
	}
	largeRaw := box{offset: 0, size: 64}
	if _, err := readRawBox(context.Background(), reader, largeRaw, &budget, "large raw"); !errors.Is(err, errUnsupportedMovie) {
		t.Fatalf("readRawBox() error = %v, want allocation rejection", err)
	}
	if reader.largestRead != 0 {
		t.Fatalf("large allocation guard read %d bytes", reader.largestRead)
	}

	payload := fixtureFullBox(0, 0, fixtureU32(1))
	payload = append(payload, fixtureU32(math.MaxUint32)...)
	stsz := fixtureBox("stsz", payload)
	stszBox := collectTopLevel(t, stsz)[0]
	reader = &recordingRandom{data: stsz}
	budget = movieBudget{readLimit: 64, remaining: 64}
	if _, err := parseSampleSizes(context.Background(), reader, stszBox, &budget); !errors.Is(err, errUnsupportedMovie) {
		t.Fatalf("parseSampleSizes() error = %v, want retained-array rejection", err)
	}
	if reader.largestRead != len(payload) {
		t.Fatalf("stsz parser read = %d, want only %d-byte table", reader.largestRead, len(payload))
	}

	reader = &recordingRandom{data: make([]byte, 16)}
	budget = movieBudget{readLimit: math.MaxUint64, remaining: 32}
	if _, err := parseSampleDescriptions(context.Background(), reader, box{payloadSize: 64}, &budget, 1); !errors.Is(err, errUnsupportedMovie) {
		t.Fatalf("parseSampleDescriptions() error = %v, want backing rejection", err)
	}
	if reader.largestRead != 0 {
		t.Fatalf("stsd backing guard read %d bytes", reader.largestRead)
	}
}
