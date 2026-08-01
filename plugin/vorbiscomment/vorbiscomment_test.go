package vorbiscomment

import (
	"encoding/binary"
	"reflect"
	"testing"

	"github.com/godexture/godec/core/domain/metadata"
	"github.com/godexture/godec/sdk/date"
)

func TestParseMapsCommentsAndKeepsUnmappedFields(t *testing.T) {
	t.Parallel()
	payload := testCommentPayload("encoder", []string{
		"title=Song", "ARTIST=First", "ARTIST=Second", "DATE=2024-06-17",
		"TRACKNUMBER=2/12", "TRACKTOTAL=99", "PERFORMER=Band", "DATE=not-a-date",
	})
	bundle := metadata.NewBundle()
	if err := Parse(payload, bundle); err != nil {
		t.Fatal(err)
	}
	if got := metadata.Get[metadata.KeyTitle](bundle); got != "Song" {
		t.Fatalf("title = %q", got)
	}
	if got := metadata.Enumerate[metadata.KeyArtist](bundle); !reflect.DeepEqual(got, []metadata.KeyArtist{"First", "Second"}) {
		t.Fatalf("artists = %#v", got)
	}
	if got := date.Partial(metadata.Get[metadata.KeyDate](bundle)).ToISOString(); got != "2024-06-17" {
		t.Fatalf("date = %q", got)
	}
	if got := metadata.Get[metadata.KeyTrackNumber](bundle); got != 2 {
		t.Fatalf("track number = %d", got)
	}
	if got := metadata.Get[metadata.KeyTotalTracks](bundle); got != 12 {
		t.Fatalf("total tracks = %d", got)
	}
	if got := metadata.Get[metadata.KeyEncoder](bundle); got != "encoder" {
		t.Fatalf("encoder = %q", got)
	}
	raw, ok := bundle.GetRaw(metadataFieldKey)
	if !ok || !reflect.DeepEqual(raw, [][]byte{[]byte("PERFORMER=Band"), []byte("DATE=not-a-date")}) {
		t.Fatalf("raw fields = %#v", raw)
	}
}

func TestParseRejectsTruncatedPayload(t *testing.T) {
	t.Parallel()
	bundle := metadata.NewBundle()
	if err := Parse([]byte{4, 0, 0, 0, 'x'}, bundle); err == nil {
		t.Fatal("Parse() error = nil")
	}
}

func TestMarshalRoundtrip(t *testing.T) {
	t.Parallel()
	bundle := metadata.NewBundle()
	bundle.Set(metadata.KeyTitle("Song"))
	bundle.PushBack(metadata.KeyArtist("First"))
	bundle.PushBack(metadata.KeyArtist("Second"))
	bundle.Set(metadata.KeyTrackNumber(2))
	bundle.Set(metadata.KeyTotalTracks(12))
	bundle.AddRaw(metadataFieldKey, []byte("PERFORMER=Band"))

	parsed := metadata.NewBundle()
	if err := Parse(Marshal(*bundle), parsed); err != nil {
		t.Fatal(err)
	}
	if got := metadata.Get[metadata.KeyTitle](parsed); got != "Song" {
		t.Fatalf("title = %q", got)
	}
	if got := metadata.Enumerate[metadata.KeyArtist](parsed); !reflect.DeepEqual(got, []metadata.KeyArtist{"First", "Second"}) {
		t.Fatalf("artists = %#v", got)
	}
	if got := metadata.Get[metadata.KeyTotalTracks](parsed); got != 12 {
		t.Fatalf("total tracks = %d", got)
	}
	raw, _ := parsed.GetRaw(metadataFieldKey)
	if !reflect.DeepEqual(raw, [][]byte{[]byte("PERFORMER=Band")}) {
		t.Fatalf("raw fields = %#v", raw)
	}
}

// TestDefaultVendorIsStableProjectIdentity is the M1 baseline for
// docs/refactor/checkpoint.md M1#4's Vorbis Comment vendor-string
// decision: it is artifact identity written into other people's files,
// not a repository path, so it deliberately does not track internal
// package moves. This pins the explicit choice made when plugins/format-
// flac (the previous, now-stale value) was merged away.
func TestDefaultVendorIsStableProjectIdentity(t *testing.T) {
	t.Parallel()
	if defaultVendor != "godexture/godec" {
		t.Fatalf("defaultVendor = %q, want %q", defaultVendor, "godexture/godec")
	}
}

func testCommentPayload(vendor string, comments []string) []byte {
	payload := appendString(nil, vendor)
	payload = binary.LittleEndian.AppendUint32(payload, uint32(len(comments)))
	for _, comment := range comments {
		payload = appendString(payload, comment)
	}
	return payload
}
