package mp4

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/godexture/godec/access"
	mediasample "github.com/godexture/godec/media/sample"
)

func audioEntryBytesFor(channels, size uint16, rate uint32) []byte {
	entry := make([]byte, audioEntryBytes)
	binary.BigEndian.PutUint16(entry[24:26], channels)
	binary.BigEndian.PutUint16(entry[26:28], size)
	binary.BigEndian.PutUint32(entry[32:36], rate)
	return entry
}

func TestParseAudioEntryDescribesLinearPCM(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		channels uint16
		size     uint16
		rate     uint32
		endian   mediasample.Endian
		want     mediasample.Description
	}{
		{
			name: "stereo little endian", channels: 2, size: 16, rate: 48_000 << 16, endian: mediasample.LittleEndian,
			want: mediasample.Description{Format: mediasample.S16Interleaved, ValidBits: 16, Rate: 48_000, Layout: mediasample.Stereo, Endian: mediasample.LittleEndian},
		},
		{
			name: "mono big endian", channels: 1, size: 16, rate: 44_100 << 16, endian: mediasample.BigEndian,
			want: mediasample.Description{Format: mediasample.S16Interleaved, ValidBits: 16, Rate: 44_100, Layout: mediasample.Mono, Endian: mediasample.BigEndian},
		},
		{name: "unsupported channel count", channels: 6, size: 16, rate: 48_000 << 16, endian: mediasample.LittleEndian},
		{name: "unsupported sample size", channels: 2, size: 24, rate: 48_000 << 16, endian: mediasample.LittleEndian},
		{name: "fractional rate", channels: 2, size: 16, rate: 48_000<<16 | 1, endian: mediasample.LittleEndian},
		{name: "zero rate", channels: 2, size: 16, rate: 0, endian: mediasample.LittleEndian},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got := parseAudioEntry(audioEntryBytesFor(testCase.channels, testCase.size, testCase.rate), testCase.endian)
			if got != testCase.want {
				t.Fatalf("parseAudioEntry() = %#v, want %#v", got, testCase.want)
			}
		})
	}
	if got := parseAudioEntry(make([]byte, audioEntryBytes-1), mediasample.LittleEndian); got != (mediasample.Description{}) {
		t.Fatalf("truncated entry = %#v", got)
	}
}

func TestLinearPCMEntryCoversTheDecodableSampleEntries(t *testing.T) {
	for _, testCase := range []struct {
		typeID boxType
		endian mediasample.Endian
		linear bool
	}{
		{typeID: typeSOWT, endian: mediasample.LittleEndian, linear: true},
		{typeID: typeTWOS, endian: mediasample.BigEndian, linear: true},
		{typeID: boxTypeOf("mp4a"), endian: mediasample.NoEndian},
		{typeID: boxTypeOf("avc1"), endian: mediasample.NoEndian},
		{typeID: boxTypeOf("in24"), endian: mediasample.NoEndian},
	} {
		endian, linear := linearPCMEntry(testCase.typeID)
		if endian != testCase.endian || linear != testCase.linear {
			t.Fatalf("linearPCMEntry(%q) = %v, %t", testCase.typeID, endian, linear)
		}
	}
}

// TestTrackPropertiesPublishAudioOnlyWithAMatchingTimescale keeps the packet
// time base and the published sample rate in agreement, so a decoder never sees
// a description its input contradicts.
func TestTrackPropertiesPublishAudioOnlyWithAMatchingTimescale(t *testing.T) {
	description := mediasample.Description{Format: mediasample.S16Interleaved, ValidBits: 16, Rate: 48_000, Layout: mediasample.Stereo, Endian: mediasample.LittleEndian}
	matching, err := trackProperties(track{codec: typeSOWT, timeScale: 48_000, audio: description})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := mediasample.FromProperties(matching); err != nil || got != description {
		t.Fatalf("matching timescale properties = %#v, %v", got, err)
	}
	mismatched, err := trackProperties(track{codec: typeSOWT, timeScale: 600, audio: description})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mediasample.FromProperties(mismatched); err == nil {
		t.Fatal("a track whose timescale is not its sample rate published an audio description")
	}
}

// TestShortPCMEntryStaysOpaque keeps a nonconforming sowt entry inspectable.
// The audio fields are what this reader cannot express, not the movie, so the
// track must remain copyable instead of failing the whole file.
func TestShortPCMEntryStaysOpaque(t *testing.T) {
	tracks := []fixtureTrack{{
		id: 1, timeScale: 48_000, handler: "soun", entryType: "sowt", size: 2,
		sttsExtra: []fixtureTiming{{count: 1, duration: 1}},
	}}
	parsed := inspectMovie(t, fixtureMovie(false, "isom", []string{"iso2"}, tracks, nil, nil))
	if len(parsed.tracks) != 1 || parsed.tracks[0].codec != typeSOWT {
		t.Fatalf("inspected tracks = %#v", parsed.tracks)
	}
	if parsed.tracks[0].audio != (mediasample.Description{}) {
		t.Fatalf("short sowt entry described audio: %#v", parsed.tracks[0].audio)
	}
	properties, err := trackProperties(parsed.tracks[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mediasample.FromProperties(properties); err == nil {
		t.Fatal("short sowt entry published a sample description")
	}
}

// TestMetaOffsetScanSeparatesStructureFromBudget keeps the iloc classification
// honest. A child list this reader cannot walk fails closed, but running out of
// Inspect budget says nothing about the content and must reach the caller
// instead of being reported as an offset index.
func TestMetaOffsetScanSeparatesStructureFromBudget(t *testing.T) {
	scan := func(t testing.TB, payload []byte, reader func([]byte) access.Random) (bool, error) {
		t.Helper()
		data := fixtureBox("meta", payload)
		value := box{
			typeID: typeMETA, size: uint64(len(data)), headerSize: 8,
			payloadOffset: 8, payloadSize: uint64(len(payload)),
		}
		return metaRecordsOffsets(t.Context(), reader(data), uint64(len(data)), value)
	}
	plain := func(data []byte) access.Random { return memoryRandom(data) }

	indexed := append(fixtureFullBox(0, 0, nil), fixtureBox("iloc", nil)...)
	if records, err := scan(t, indexed, plain); err != nil || !records {
		t.Fatalf("iloc meta = %t, %v", records, err)
	}
	tagged := append(fixtureFullBox(0, 0, nil), fixtureBox("hdlr", make([]byte, 20))...)
	if records, err := scan(t, tagged, plain); err != nil || records {
		t.Fatalf("vocabulary meta = %t, %v", records, err)
	}
	// A QuickTime meta has no version and flags, so skipping four bytes lands
	// inside a child header and the list cannot be walked.
	if records, err := scan(t, fixtureBox("hdlr", make([]byte, 20)), plain); err != nil || !records {
		t.Fatalf("unwalkable meta = %t, %v", records, err)
	}
	budgeted := func(data []byte) access.Random { return newInspectReader(memoryRandom(data), 4) }
	if _, err := scan(t, indexed, budgeted); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("exhausted Inspect budget = %v, want unsupported", err)
	}
}
