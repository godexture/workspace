package mp4

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"testing"
)

func TestParseMovieRejectsMovieStructure(t *testing.T) {
	fixture := fixtureTrack{id: 1, timeScale: 1000, handler: "vide", entryType: "avc1", size: 1, sttsExtra: []fixtureTiming{{count: 1, duration: 1}}}
	for _, test := range []struct {
		name string
		moov []byte
		want error
	}{
		{name: "missing mvhd", moov: fixtureContainer("moov", fixtureTrackBox(fixture)), want: errMalformedMovie},
		{name: "duplicate mvhd", moov: fixtureContainer("moov", fixtureMVHD(), fixtureMVHD(), fixtureTrackBox(fixture)), want: errMalformedMovie},
		{name: "fragment", moov: fixtureContainer("moov", fixtureMVHD(), fixtureBox("mvex", nil), fixtureTrackBox(fixture)), want: errUnsupportedMovie},
	} {
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
		{name: "sample count mismatch", track: fixtureTrack{id: 1, timeScale: 1000, handler: "vide", entryType: "avc1", size: 1, sttsExtra: []fixtureTiming{{count: 2, duration: 1}}}, want: errMalformedMovie},
		{name: "zero timing run", track: fixtureTrack{id: 1, timeScale: 1000, handler: "vide", entryType: "avc1", size: 1, sttsExtra: []fixtureTiming{{count: 0, duration: 1}}}, want: errMalformedMovie},
		{name: "description index", track: fixtureTrack{id: 1, timeScale: 1000, handler: "vide", entryType: "avc1", size: 1, sttsExtra: []fixtureTiming{{count: 1, duration: 1}}, stsc: []fixtureChunk{{first: 1, samples: 1, description: 2}}}, want: errMalformedMovie},
		{name: "sample outside mdat", track: fixtureTrack{id: 1, timeScale: 1000, handler: "vide", entryType: "avc1", size: 1, offsetDelta: 100, sttsExtra: []fixtureTiming{{count: 1, duration: 1}}}, want: errMalformedMovie},
		{name: "unknown stbl child", track: fixtureTrack{id: 1, timeScale: 1000, handler: "vide", entryType: "avc1", size: 1, sttsExtra: []fixtureTiming{{count: 1, duration: 1}}, stblExtra: [][]byte{fixtureBox("free", nil)}}, want: errUnsupportedMovie},
		{name: "encrypted entry", track: fixtureTrack{id: 1, timeScale: 1000, handler: "vide", entryType: "encv", size: 1, sttsExtra: []fixtureTiming{{count: 1, duration: 1}}}, want: errUnsupportedMovie},
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

func TestTimingScanRejectsOverflowWithoutExpandingSamples(t *testing.T) {
	stts := fixtureSTTS([]fixtureTiming{{count: math.MaxUint32, duration: math.MaxUint32}, {count: math.MaxUint32, duration: math.MaxUint32}})
	value := collectTopLevel(t, stts)[0]
	if _, _, err := scanTimingTable(context.Background(), memoryRandom(stts), value); !errors.Is(err, errMalformedMovie) {
		t.Fatalf("scanTimingTable() error = %v, want malformed overflow", err)
	}
}

func TestSampleTableScanUsesFixedReads(t *testing.T) {
	payload := fixtureFullBox(0, 0, fixtureU32(0))
	payload = append(payload, fixtureU32(math.MaxUint32)...)
	stsz := fixtureBox("stsz", payload)
	value := collectTopLevel(t, stsz)[0]
	reader := &recordingRandom{data: stsz}
	if _, _, _, err := scanSampleSizes(context.Background(), reader, value, false); !errors.Is(err, errMalformedMovie) {
		t.Fatalf("scanSampleSizes() error = %v, want malformed", err)
	}
	if reader.largestRead > 12 {
		t.Fatalf("sample-size scan read %d bytes, want fixed header/page", reader.largestRead)
	}
}

func TestSampleTableScanUsesBoundedPages(t *testing.T) {
	entries := make([]fixtureTiming, tablePageBytes/8*3+1)
	for index := range entries {
		entries[index] = fixtureTiming{count: 1, duration: 1}
	}
	stts := fixtureSTTS(entries)
	value := collectTopLevel(t, stts)[0]
	reader := &recordingRandom{data: stts}
	if _, _, err := scanTimingTable(context.Background(), reader, value); err != nil {
		t.Fatal(err)
	}
	if reader.largestRead > tablePageBytes {
		t.Fatalf("table scan read %d bytes, page is %d", reader.largestRead, tablePageBytes)
	}
	if reader.readCalls > 5 {
		t.Fatalf("table scan made %d reads, want header plus bounded pages", reader.readCalls)
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
	binary.BigEndian.PutUint32(mvhd[20:24], 1000)
	binary.BigEndian.PutUint64(mvhd[24:32], 4000)
	header, err := parseMovieHeader(mvhd, box{payloadOffset: 40})
	if err != nil || header.timeScale != 1000 || header.duration != (durationField{offset: 64, value: 4000, wide: true}) {
		t.Fatalf("parseMovieHeader() = %#v, %v", header, err)
	}
	if _, err := parseMovieHeader(mvhd[:111], box{}); !errors.Is(err, errMalformedMovie) {
		t.Fatalf("parseMovieHeader() error = %v, want malformed", err)
	}

	tkhd := make([]byte, 96)
	tkhd[0] = 1
	binary.BigEndian.PutUint32(tkhd[20:24], 1)
	binary.BigEndian.PutUint64(tkhd[28:36], 2000)
	track, err := parseTrackHeader(tkhd, box{payloadOffset: 40})
	if err != nil || track.id != 1 || track.duration != (durationField{offset: 68, value: 2000, wide: true}) {
		t.Fatalf("parseTrackHeader() = %#v, %v", track, err)
	}
	if _, err := parseTrackHeader(tkhd[:95], box{}); !errors.Is(err, errMalformedMovie) {
		t.Fatalf("parseTrackHeader() error = %v, want malformed", err)
	}

	mdhd := make([]byte, 36)
	mdhd[0] = 1
	binary.BigEndian.PutUint32(mdhd[20:24], 1000)
	if timeScale, err := parseTimeScale(mdhd); err != nil || timeScale != 1000 {
		t.Fatalf("parseTimeScale() = %d, %v", timeScale, err)
	}
	if _, err := parseTimeScale(mdhd[:35]); !errors.Is(err, errMalformedMovie) {
		t.Fatalf("parseTimeScale() error = %v, want malformed", err)
	}
}
