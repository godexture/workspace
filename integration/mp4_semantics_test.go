package integration_test

import (
	"encoding/binary"
	"testing"
)

type mp4FixtureBoxView struct {
	typeID  string
	start   int
	payload []byte
}

func mp4FixtureTopLevel(value []byte) []mp4FixtureBoxView {
	return mp4FixtureChildren(value, 0, len(value))
}

func mp4FixtureChildren(value []byte, start, end int) []mp4FixtureBoxView {
	var result []mp4FixtureBoxView
	if start < 0 || end < start || end > len(value) {
		return nil
	}
	for offset := start; offset < end; {
		if end-offset < 8 {
			return nil
		}
		size := int(binary.BigEndian.Uint32(value[offset : offset+4]))
		if size < 8 || size > end-offset {
			return nil
		}
		result = append(result, mp4FixtureBoxView{
			typeID:  string(value[offset+4 : offset+8]),
			start:   offset,
			payload: value[offset+8 : offset+size],
		})
		offset += size
	}
	return result
}

func mp4FixtureChild(value mp4FixtureBoxView, typeID string) (mp4FixtureBoxView, bool) {
	for _, child := range mp4FixtureChildren(value.payload, 0, len(value.payload)) {
		if child.typeID == typeID {
			return child, true
		}
	}
	return mp4FixtureBoxView{}, false
}

func assertMP4FixtureSemantics(t testing.TB, value []byte) {
	t.Helper()
	top := mp4FixtureTopLevel(value)
	if len(top) != 3 || top[0].typeID != "ftyp" || top[1].typeID != "moov" || top[2].typeID != "mdat" {
		t.Fatalf("MP4 output top-level boxes = %#v", top)
	}
	var tracks []mp4FixtureBoxView
	for _, child := range mp4FixtureChildren(top[1].payload, 0, len(top[1].payload)) {
		if child.typeID == "trak" {
			tracks = append(tracks, child)
		}
	}
	if len(tracks) != 2 {
		t.Fatalf("MP4 output tracks = %d, want 2", len(tracks))
	}
	want := []struct {
		timeScale   uint32
		duration    uint32
		entry       string
		payload     []byte
		composition int32
	}{
		{timeScale: 48_000, duration: 1024, entry: "zzzz", payload: []byte{0xde, 0xad}},
		{timeScale: 1_000, duration: 40, entry: "avc1", payload: []byte{0xca, 0xfe, 0xba}, composition: 3},
	}
	for index, track := range tracks {
		mdia, ok := mp4FixtureChild(track, "mdia")
		if !ok {
			t.Fatalf("MP4 track %d has no mdia", index)
		}
		mdhd, ok := mp4FixtureChild(mdia, "mdhd")
		if !ok || len(mdhd.payload) < 16 || binary.BigEndian.Uint32(mdhd.payload[12:16]) != want[index].timeScale {
			t.Fatalf("MP4 track %d time scale = %#v", index, mdhd)
		}
		minf, ok := mp4FixtureChild(mdia, "minf")
		stbl, stblOK := mp4FixtureChild(minf, "stbl")
		if !ok || !stblOK {
			t.Fatalf("MP4 track %d sample table is absent", index)
		}
		stsd, ok := mp4FixtureChild(stbl, "stsd")
		if !ok || len(stsd.payload) < 24 || string(stsd.payload[12:16]) != want[index].entry {
			t.Fatalf("MP4 track %d sample entry = %#v", index, stsd)
		}
		stts, ok := mp4FixtureChild(stbl, "stts")
		if !ok || len(stts.payload) < 16 || binary.BigEndian.Uint32(stts.payload[12:16]) != want[index].duration {
			t.Fatalf("MP4 track %d timing = %#v", index, stts)
		}
		ctts, hasCTTS := mp4FixtureChild(stbl, "ctts")
		if want[index].composition == 0 {
			if hasCTTS {
				t.Fatalf("MP4 track %d unexpectedly has composition timing", index)
			}
		} else if !hasCTTS || len(ctts.payload) < 16 || int32(binary.BigEndian.Uint32(ctts.payload[12:16])) != want[index].composition {
			t.Fatalf("MP4 track %d composition timing = %#v", index, ctts)
		}
	}
	// Where each track's bytes sit is the source's business, not this
	// assertion's: what has to hold is that every chunk-offset entry addresses
	// its own track's payload and that the chunks cover mdat exactly once.
	payloads := make([][]byte, len(want))
	for index := range want {
		payloads[index] = want[index].payload
	}
	assertMP4ChunkTablesTileTheMedia(t, value, payloads)
}
