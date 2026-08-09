package wave

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/godexture/godec/host"
	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/plugin"
)

func TestRIFFInfoEncodingPreservesDuplicatesUnknownFieldsAndPadding(t *testing.T) {
	title := infoTestChunk(t, "INAM", []byte("Song\x00"), 0x7f)
	artistFirst := infoTestChunk(t, "IART", []byte("First\x00"), 0)
	artistSecond := infoTestChunk(t, "IART", []byte("Second\x00"), 0xa5)
	unknown := infoTestChunk(t, "XTRA", []byte{1, 2, 3}, 0xcc)
	value := infoTestList(t, title, artistFirst, artistSecond, unknown)
	resolver := infoTestResolver(t)

	document, err := resolver.Parse(t.Context(), RIFFInfo(), "list-0", metadata.StreamScope, metadata.NewBlob("application/x-riff-info", value))
	if err != nil {
		t.Fatal(err)
	}
	entries := document.Entries()
	if len(entries) != 3 || entries[0].Key() != tag.Title().ID() || entries[1].Key() != tag.Artist().ID() || entries[2].Key() != tag.Artist().ID() {
		t.Fatalf("RIFF INFO entry order = %#v", entries)
	}
	if artists := metadata.Values(document, tag.Artist()); len(artists) != 2 || artists[0] != "First" || artists[1] != "Second" {
		t.Fatalf("RIFF INFO artists = %v", artists)
	}
	for _, entry := range entries {
		origin := entry.Origin()
		if origin.Encoding != InfoEncodingIdentity() || origin.Carrier != RIFFInfo() || origin.Block != "list-0" || origin.Native == "" {
			t.Fatalf("RIFF INFO origin = %#v", origin)
		}
	}
	blocks := document.Blocks()
	if len(blocks) != 2 || blocks[0].ID() != "list-0" || !bytes.Equal(blocks[0].Payload().AppendTo(nil), value) {
		t.Fatalf("RIFF INFO raw blocks = %#v", blocks)
	}
	if !bytes.Equal(blocks[1].Payload().AppendTo(nil), unknown) {
		t.Fatalf("unknown INFO field = %x, want %x", blocks[1].Payload().AppendTo(nil), unknown)
	}

	encoded, err := resolver.Marshal(t.Context(), RIFFInfo(), "list-0", document)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded.AppendTo(nil), value) {
		t.Fatalf("unchanged RIFF INFO = %x, want %x", encoded.AppendTo(nil), value)
	}
}

func TestRIFFInfoEncodingBuildsSemanticDocumentInEntryOrder(t *testing.T) {
	builder := metadata.NewBuilder(metadata.StreamScope)
	metadata.Add(builder, tag.Title(), "Song", metadata.Origin{})
	metadata.Add(builder, tag.Artist(), "First", metadata.Origin{})
	metadata.Add(builder, tag.Artist(), "Second", metadata.Origin{})
	date, err := tag.ParseDate("2026-08-10")
	if err != nil {
		t.Fatal(err)
	}
	metadata.Add(builder, tag.Date(), date, metadata.Origin{})
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	resolver := infoTestResolver(t)
	encoded, err := resolver.Marshal(t.Context(), RIFFInfo(), "new-list", document)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := resolver.Parse(t.Context(), RIFFInfo(), "new-list", metadata.StreamScope, encoded)
	if err != nil {
		t.Fatal(err)
	}
	entries := parsed.Entries()
	if len(entries) != 4 || entries[0].Key() != tag.Title().ID() || entries[1].Key() != tag.Artist().ID() || entries[2].Key() != tag.Artist().ID() || entries[3].Key() != tag.Date().ID() {
		t.Fatalf("synthesized RIFF INFO order = %#v", entries)
	}
	if artists := metadata.Values(parsed, tag.Artist()); len(artists) != 2 || artists[0] != "First" || artists[1] != "Second" {
		t.Fatalf("synthesized RIFF INFO artists = %v", artists)
	}
}

func TestRIFFInfoEncodingAcceptsEmptyListAndRejectsMalformedChunks(t *testing.T) {
	resolver := infoTestResolver(t)
	empty := infoTestList(t)
	document, err := resolver.Parse(t.Context(), RIFFInfo(), "empty", metadata.StreamScope, metadata.NewBlob("", empty))
	if err != nil || document.Len() != 0 || len(document.Blocks()) != 1 {
		t.Fatalf("empty RIFF INFO = %#v, %v", document, err)
	}

	wrongType, err := marshalInfoChunk(tagLIST, []byte("NOPE"))
	if err != nil {
		t.Fatal(err)
	}
	badSize := append([]byte(nil), empty...)
	binary.LittleEndian.PutUint32(badSize[4:8], binary.LittleEndian.Uint32(badSize[4:8])+1)
	truncatedField, err := marshalInfoChunk(tagLIST, append([]byte(tagINFO+"XTRA\x04\x00\x00\x00"), 1, 2))
	if err != nil {
		t.Fatal(err)
	}
	missingFieldPad, err := marshalInfoChunk(tagLIST, append([]byte(tagINFO+"XTRA\x01\x00\x00\x00"), 1))
	if err != nil {
		t.Fatal(err)
	}
	tests := [][]byte{
		nil,
		[]byte("LIST"),
		wrongType,
		badSize,
		infoTestList(t, []byte{1, 2, 3}),
		truncatedField,
		missingFieldPad,
	}
	for index, value := range tests {
		if _, err := resolver.Parse(t.Context(), RIFFInfo(), "broken", metadata.StreamScope, metadata.NewBlob("", value)); !errors.Is(err, ErrMalformed) {
			t.Fatalf("malformed RIFF INFO %d error = %v", index, err)
		}
	}
}

func TestWAVESetCarriesRIFFInfoEncodingAndBinding(t *testing.T) {
	instance, err := host.New(host.Plugins(Set()))
	if err != nil {
		t.Fatal(err)
	}
	view, ok := instance.Catalog().Lookup(InfoEncodingIdentity())
	if !ok || view.Executable || view.HasSpec || len(view.Traits) != 1 {
		t.Fatalf("RIFF INFO component = %#v/%v", view, ok)
	}
	carriers := WAVE().Carriers()
	if len(carriers) != 1 || carriers[0] != RIFFInfo() {
		t.Fatalf("WAVE carriers = %v", carriers)
	}
}

func FuzzRIFFInfoEncodingRoundTripsAcceptedCarriers(f *testing.F) {
	f.Add(infoTestList(f))
	f.Add(infoTestList(f, infoTestChunk(f, "INAM", []byte("Song\x00"), 0)))
	f.Add([]byte(nil))
	resolver := infoTestResolver(f)
	f.Fuzz(func(t *testing.T, value []byte) {
		document, err := resolver.Parse(t.Context(), RIFFInfo(), "fuzz", metadata.StreamScope, metadata.NewBlob("", value))
		if err != nil {
			return
		}
		encoded, err := resolver.Marshal(t.Context(), RIFFInfo(), "fuzz", document)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(encoded.AppendTo(nil), value) {
			t.Fatalf("accepted carrier changed: %x != %x", encoded.AppendTo(nil), value)
		}
	})
}

func infoTestResolver(t testing.TB) metadata.Resolver {
	t.Helper()
	resolver, err := metadata.NewResolver(map[carrier.ID]plugin.Component{RIFFInfo(): infoComponent()})
	if err != nil {
		t.Fatal(err)
	}
	return resolver
}

func infoTestList(t testing.TB, fields ...[]byte) []byte {
	t.Helper()
	payload := []byte(tagINFO)
	for _, field := range fields {
		payload = append(payload, field...)
	}
	value, err := marshalInfoChunk(tagLIST, payload)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func infoTestChunk(t testing.TB, native string, payload []byte, padding byte) []byte {
	t.Helper()
	value, err := marshalInfoChunk(native, payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload)&1 != 0 {
		value[len(value)-1] = padding
	}
	return value
}
