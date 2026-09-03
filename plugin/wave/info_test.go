package wave

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/godexture/godec/host"
	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/plugin"
)

type (
	infoOtherCarrierID  struct{}
	infoOtherEncodingID struct{}
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
	if len(blocks) != 2 || blocks[0].ID() != "list-0" || !blocks[0].Source() || !bytes.Equal(blocks[0].Payload().AppendTo(nil), value) {
		t.Fatalf("RIFF INFO raw blocks = %#v", blocks)
	}
	if blocks[1].Source() || !bytes.Equal(blocks[1].Payload().AppendTo(nil), unknown) {
		t.Fatalf("unknown INFO field = %x, want %x", blocks[1].Payload().AppendTo(nil), unknown)
	}

	encoded, _, err := resolver.Marshal(t.Context(), RIFFInfo(), "list-0", metadata.MustAvailable(document))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded.AppendTo(nil), value) {
		t.Fatalf("unchanged RIFF INFO = %x, want %x", encoded.AppendTo(nil), value)
	}
}

func TestRIFFInfoEncodingAppendsEditsWithoutDiscardingOriginalBytes(t *testing.T) {
	title := infoTestChunk(t, "INAM", []byte("Song\x00"), 0x7f)
	unknown := infoTestChunk(t, "XTRA", []byte{1, 2, 3}, 0xcc)
	original := infoTestList(t, title, unknown)
	resolver := infoTestResolver(t)
	document, err := resolver.Parse(t.Context(), RIFFInfo(), "list-0", metadata.StreamScope, metadata.NewBlob("application/x-riff-info", original))
	if err != nil {
		t.Fatal(err)
	}
	edited, err := metadata.Add(document.Edit(), tag.Comment(), "added", metadata.Origin{}).Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, _, err := resolver.Marshal(t.Context(), RIFFInfo(), "list-0", metadata.MustAvailable(edited))
	if err != nil {
		t.Fatal(err)
	}
	want := infoTestList(t, title, unknown, infoTestChunk(t, "ICMT", []byte("added\x00"), 0))
	if !bytes.Equal(encoded.AppendTo(nil), want) {
		t.Fatalf("edited RIFF INFO = %x, want %x", encoded.AppendTo(nil), want)
	}
	parsed, err := resolver.Parse(t.Context(), RIFFInfo(), "list-0", metadata.StreamScope, encoded)
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := metadata.First(parsed, tag.Comment()); !ok || value != "added" {
		t.Fatalf("edited comment = %q/%v", value, ok)
	}
	if blocks := parsed.Blocks(); len(blocks) != 2 || !bytes.Equal(blocks[1].Payload().AppendTo(nil), unknown) {
		t.Fatalf("edited unknown block = %#v", blocks)
	}
}

func TestRIFFInfoEncodingKeepsUnknownSlotsWhenSemanticEntriesAreRemoved(t *testing.T) {
	title := infoTestChunk(t, "INAM", []byte("Song\x00"), 0x7f)
	unknown := infoTestChunk(t, "XTRA", []byte{1, 2, 3}, 0xcc)
	artist := infoTestChunk(t, "IART", []byte("Artist\x00"), 0xa5)
	original := infoTestList(t, title, unknown, artist)
	resolver := infoTestResolver(t)
	document, err := resolver.Parse(t.Context(), RIFFInfo(), "list-0", metadata.StreamScope, metadata.NewBlob("application/x-riff-info", original))
	if err != nil {
		t.Fatal(err)
	}
	builder := metadata.NewBuilder(metadata.StreamScope)
	for _, block := range document.Blocks() {
		builder.AddBlock(block)
	}
	for _, entry := range document.Entries() {
		if entry.Key() == tag.Artist().ID() {
			metadata.Add(builder, tag.Artist(), entry.Value().(string), entry.Origin())
		}
	}
	edited, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, _, err := resolver.Marshal(t.Context(), RIFFInfo(), "list-0", metadata.MustAvailable(edited))
	if err != nil {
		t.Fatal(err)
	}
	want := infoTestList(t, unknown, artist)
	if !bytes.Equal(encoded.AppendTo(nil), want) {
		t.Fatalf("removed RIFF INFO entry = %x, want %x", encoded.AppendTo(nil), want)
	}
}

func TestRIFFInfoEncodingPreservesUnchangedRawSlotsWhenAnotherEntryChanges(t *testing.T) {
	title := infoTestChunk(t, "INAM", []byte("Song\x00\x00"), 0)
	artist := infoTestChunk(t, "IART", []byte("Original\x00"), 0xa5)
	original := infoTestList(t, title, artist)
	resolver := infoTestResolver(t)
	document, err := resolver.Parse(t.Context(), RIFFInfo(), "list-0", metadata.StreamScope, metadata.NewBlob("application/x-riff-info", original))
	if err != nil {
		t.Fatal(err)
	}
	builder := metadata.NewBuilder(metadata.StreamScope)
	for _, block := range document.Blocks() {
		builder.AddBlock(block)
	}
	for _, entry := range document.Entries() {
		switch entry.Key() {
		case tag.Title().ID():
			metadata.Add(builder, tag.Title(), entry.Value().(string), entry.Origin())
		case tag.Artist().ID():
			metadata.Add(builder, tag.Artist(), "Edited", entry.Origin())
		}
	}
	edited, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, _, err := resolver.Marshal(t.Context(), RIFFInfo(), "list-0", metadata.MustAvailable(edited))
	if err != nil {
		t.Fatal(err)
	}
	want := infoTestList(t, title, infoTestChunk(t, "IART", []byte("Edited\x00"), 0))
	if !bytes.Equal(encoded.AppendTo(nil), want) {
		t.Fatalf("edited RIFF INFO = %x, want %x", encoded.AppendTo(nil), want)
	}
}

func TestRIFFInfoEncodingMatchesDuplicateNativeEntriesByOriginAndValue(t *testing.T) {
	first := infoTestChunk(t, "IART", []byte("First\x00"), 0x7f)
	unknown := infoTestChunk(t, "XTRA", []byte{1, 2, 3}, 0xcc)
	second := infoTestChunk(t, "IART", []byte("Second\x00"), 0xa5)
	original := infoTestList(t, first, unknown, second)
	resolver := infoTestResolver(t)
	document, err := resolver.Parse(t.Context(), RIFFInfo(), "list-0", metadata.StreamScope, metadata.NewBlob("application/x-riff-info", original))
	if err != nil {
		t.Fatal(err)
	}
	builder := metadata.NewBuilder(metadata.StreamScope)
	for _, block := range document.Blocks() {
		builder.AddBlock(block)
	}
	for index, entry := range document.Entries() {
		if entry.Key() != tag.Artist().ID() {
			continue
		}
		if index == 0 {
			metadata.Add(builder, tag.Artist(), "Edited", entry.Origin())
			continue
		}
		metadata.Add(builder, tag.Artist(), entry.Value().(string), entry.Origin())
	}
	edited, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, _, err := resolver.Marshal(t.Context(), RIFFInfo(), "list-0", metadata.MustAvailable(edited))
	if err != nil {
		t.Fatal(err)
	}
	want := infoTestList(t, infoTestChunk(t, "IART", []byte("Edited\x00"), 0), unknown, second)
	if !bytes.Equal(encoded.AppendTo(nil), want) {
		t.Fatalf("edited duplicate RIFF INFO = %x, want %x", encoded.AppendTo(nil), want)
	}
}

func TestRIFFInfoEncodingTracksUnknownChildBlockEdits(t *testing.T) {
	title := infoTestChunk(t, "INAM", []byte("Song\x00"), 0x7f)
	unknown := infoTestChunk(t, "XTRA", []byte{1, 2, 3}, 0xcc)
	original := infoTestList(t, title, unknown)
	resolver := infoTestResolver(t)
	document, err := resolver.Parse(t.Context(), RIFFInfo(), "list-0", metadata.StreamScope, metadata.NewBlob("application/x-riff-info", original))
	if err != nil {
		t.Fatal(err)
	}
	child := document.Blocks()[1]
	changed := infoTestChunk(t, "YNEW", []byte{9, 8, 7, 6, 5}, 0x5a)
	for _, test := range []struct {
		name  string
		block *metadata.RawBlock
		want  []byte
	}{
		{name: "changed", block: func() *metadata.RawBlock {
			value := metadata.NewRawBlock(child.ID(), child.Carrier(), child.Encoding(), metadata.NewBlob("application/octet-stream", changed))
			return &value
		}(), want: infoTestList(t, title, changed)},
		{name: "removed", want: infoTestList(t, title)},
		{name: "appended", block: func() *metadata.RawBlock {
			value := metadata.NewRawBlock("list-0/field/99999999", child.Carrier(), child.Encoding(), metadata.NewBlob("application/octet-stream", infoTestChunk(t, "NEW!", []byte{4, 5}, 0)))
			return &value
		}(), want: infoTestList(t, title, unknown, infoTestChunk(t, "NEW!", []byte{4, 5}, 0))},
	} {
		t.Run(test.name, func(t *testing.T) {
			builder := metadata.NewBuilder(metadata.StreamScope)
			for _, block := range document.Blocks() {
				if test.name == "removed" && block.ID() == child.ID() {
					continue
				}
				if test.block != nil && block.ID() == test.block.ID() {
					block = *test.block
				}
				builder.AddBlock(block)
			}
			if test.block != nil {
				if _, exists := document.Block(test.block.ID()); !exists {
					builder.AddBlock(*test.block)
				}
			}
			copyInfoEntries(builder, document.Entries())
			edited, err := builder.Build()
			if err != nil {
				t.Fatal(err)
			}
			encoded, _, err := resolver.Marshal(t.Context(), RIFFInfo(), "list-0", metadata.MustAvailable(edited))
			if err != nil {
				t.Fatal(err)
			}
			if got := encoded.AppendTo(nil); !bytes.Equal(got, test.want) {
				t.Fatalf("unknown child edit = %x, want %x", got, test.want)
			}
		})
	}
}

func TestRIFFInfoEncodingAppendsNewChildrenInDocumentOrder(t *testing.T) {
	title := infoTestChunk(t, "INAM", []byte("Song\x00"), 0x7f)
	unknown := infoTestChunk(t, "XTRA", []byte{1, 2, 3}, 0xcc)
	original := infoTestList(t, title, unknown)
	resolver := infoTestResolver(t)
	document, err := resolver.Parse(t.Context(), RIFFInfo(), "list-0", metadata.StreamScope, metadata.NewBlob("application/x-riff-info", original))
	if err != nil {
		t.Fatal(err)
	}
	child := document.Blocks()[1]
	first := infoTestChunk(t, "A001", []byte{1, 2, 3, 4}, 0)
	second := infoTestChunk(t, "B002", []byte{5}, 0xa1)
	firstBlock := metadata.NewRawBlock("list-0/field/90000000", child.Carrier(), child.Encoding(), metadata.NewBlob("application/octet-stream", first))
	secondBlock := metadata.NewRawBlock("list-0/field/10000000", child.Carrier(), child.Encoding(), metadata.NewBlob("application/octet-stream", second))
	builder := metadata.NewBuilder(metadata.StreamScope)
	for _, block := range document.Blocks() {
		builder.AddBlock(block)
	}
	builder.AddBlock(firstBlock).AddBlock(secondBlock)
	copyInfoEntries(builder, document.Entries())
	edited, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, _, err := resolver.Marshal(t.Context(), RIFFInfo(), "list-0", metadata.MustAvailable(edited))
	if err != nil {
		t.Fatal(err)
	}
	want := infoTestList(t, title, unknown, first, second)
	if got := encoded.AppendTo(nil); !bytes.Equal(got, want) {
		t.Fatalf("new child document order = %x, want %x", got, want)
	}
}

func TestRIFFInfoEncodingRejectsForeignChildProvenance(t *testing.T) {
	title := infoTestChunk(t, "INAM", []byte("Song\x00"), 0x7f)
	unknown := infoTestChunk(t, "XTRA", []byte{1, 2, 3}, 0xcc)
	resolver := infoTestResolver(t)
	document, err := resolver.Parse(t.Context(), RIFFInfo(), "list-0", metadata.StreamScope, metadata.NewBlob("application/x-riff-info", infoTestList(t, title, unknown)))
	if err != nil {
		t.Fatal(err)
	}
	child := document.Blocks()[1]
	for _, test := range []struct {
		name     string
		carrier  carrier.ID
		encoding plugin.Identity
		source   bool
	}{
		{name: "carrier", carrier: carrier.Define[infoOtherCarrierID](), encoding: child.Encoding()},
		{name: "encoding", carrier: child.Carrier(), encoding: plugin.IdentityOf[infoOtherEncodingID]()},
		{name: "source", carrier: child.Carrier(), encoding: child.Encoding(), source: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			builder := metadata.NewBuilder(metadata.StreamScope)
			for _, block := range document.Blocks() {
				if block.ID() == child.ID() {
					if test.source {
						block = metadata.NewSourceBlock(block.ID(), test.carrier, test.encoding, block.Payload())
					} else {
						block = metadata.NewRawBlock(block.ID(), test.carrier, test.encoding, block.Payload())
					}
				}
				builder.AddBlock(block)
			}
			copyInfoEntries(builder, document.Entries())
			edited, err := builder.Build()
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = resolver.Marshal(t.Context(), RIFFInfo(), "list-0", metadata.MustAvailable(edited))
			if !errors.Is(err, ErrMalformed) {
				t.Fatalf("foreign %s child error = %v, want ErrMalformed", test.name, err)
			}
		})
	}
}

func TestRIFFInfoEncodingRejectsMalformedChangedChild(t *testing.T) {
	title := infoTestChunk(t, "INAM", []byte("Song\x00"), 0x7f)
	unknown := infoTestChunk(t, "XTRA", []byte{1, 2, 3}, 0xcc)
	resolver := infoTestResolver(t)
	document, err := resolver.Parse(t.Context(), RIFFInfo(), "list-0", metadata.StreamScope, metadata.NewBlob("application/x-riff-info", infoTestList(t, title, unknown)))
	if err != nil {
		t.Fatal(err)
	}
	child := document.Blocks()[1]
	malformed := child.Payload().AppendTo(nil)
	binary.LittleEndian.PutUint32(malformed[4:8], 99)
	replacement := metadata.NewRawBlock(child.ID(), child.Carrier(), child.Encoding(), metadata.NewBlob("application/octet-stream", malformed))
	builder := metadata.NewBuilder(metadata.StreamScope)
	for _, block := range document.Blocks() {
		if block.ID() == child.ID() {
			block = replacement
		}
		builder.AddBlock(block)
	}
	copyInfoEntries(builder, document.Entries())
	edited, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = resolver.Marshal(t.Context(), RIFFInfo(), "list-0", metadata.MustAvailable(edited))
	if !errors.Is(err, ErrMalformed) {
		t.Fatalf("malformed changed child error = %v, want ErrMalformed", err)
	}
}

func TestRIFFInfoEncodingRespectsDuplicateDocumentOrder(t *testing.T) {
	first := infoTestChunk(t, "IART", []byte("First\x00"), 0x7f)
	unknown := infoTestChunk(t, "XTRA", []byte{1, 2, 3}, 0xcc)
	second := infoTestChunk(t, "IART", []byte("Second\x00"), 0xa5)
	original := infoTestList(t, first, unknown, second)
	resolver := infoTestResolver(t)
	document, err := resolver.Parse(t.Context(), RIFFInfo(), "list-0", metadata.StreamScope, metadata.NewBlob("application/x-riff-info", original))
	if err != nil {
		t.Fatal(err)
	}
	builder := metadata.NewBuilder(metadata.StreamScope)
	for _, block := range document.Blocks() {
		builder.AddBlock(block)
	}
	entries := document.Entries()
	metadata.Add(builder, tag.Artist(), entries[1].Value().(string), entries[1].Origin())
	metadata.Add(builder, tag.Artist(), entries[0].Value().(string), entries[0].Origin())
	reordered, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, _, err := resolver.Marshal(t.Context(), RIFFInfo(), "list-0", metadata.MustAvailable(reordered))
	if err != nil {
		t.Fatal(err)
	}
	want := infoTestList(t, infoTestChunk(t, "IART", []byte("Second\x00"), 0), unknown, infoTestChunk(t, "IART", []byte("First\x00"), 0))
	if got := encoded.AppendTo(nil); !bytes.Equal(got, want) {
		t.Fatalf("duplicate document order = %x, want %x", got, want)
	}
}

func TestRIFFInfoEncodingDoesNotSilentlyReturnRawForReplacement(t *testing.T) {
	title := infoTestChunk(t, "INAM", []byte("Song\x00"), 0x7f)
	unknown := infoTestChunk(t, "XTRA", []byte{1, 2, 3}, 0xcc)
	original := infoTestList(t, title, unknown)
	resolver := infoTestResolver(t)
	parsed, err := resolver.Parse(t.Context(), RIFFInfo(), "list-0", metadata.StreamScope, metadata.NewBlob("application/x-riff-info", original))
	if err != nil {
		t.Fatal(err)
	}
	builder := metadata.NewBuilder(metadata.StreamScope)
	for _, block := range parsed.Blocks() {
		builder.AddBlock(block)
	}
	metadata.Add(builder, tag.Comment(), "replacement", metadata.Origin{})
	replaced, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, _, err := resolver.Marshal(t.Context(), RIFFInfo(), "list-0", metadata.MustAvailable(replaced))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(encoded.AppendTo(nil), original) {
		t.Fatalf("replacement unexpectedly returned original bytes: %x", encoded.AppendTo(nil))
	}
	if bytes.Contains(encoded.AppendTo(nil), []byte("Song")) {
		t.Fatalf("removed semantic value survived replacement: %x", encoded.AppendTo(nil))
	}
	if !bytes.Contains(encoded.AppendTo(nil), unknown) {
		t.Fatalf("unknown raw subchunk was discarded: %x", encoded.AppendTo(nil))
	}
	roundTrip, err := resolver.Parse(t.Context(), RIFFInfo(), "list-0", metadata.StreamScope, encoded)
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := metadata.First(roundTrip, tag.Comment()); !ok || value != "replacement" {
		t.Fatalf("replacement comment = %q/%v", value, ok)
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
	encoded, _, err := resolver.Marshal(t.Context(), RIFFInfo(), "new-list", metadata.MustAvailable(document))
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
		encoded, _, err := resolver.Marshal(t.Context(), RIFFInfo(), "fuzz", metadata.MustAvailable(document))
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
	resolver, err := metadata.NewResolver(map[carrier.ID]plugin.Component{RIFFInfo(): infoComponent()}, nil)
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

func copyInfoEntries(builder *metadata.Builder, entries []metadata.Entry) {
	for _, entry := range entries {
		switch entry.Key() {
		case tag.Title().ID():
			metadata.Add(builder, tag.Title(), entry.Value().(string), entry.Origin())
		case tag.Artist().ID():
			metadata.Add(builder, tag.Artist(), entry.Value().(string), entry.Origin())
		case tag.Album().ID():
			metadata.Add(builder, tag.Album(), entry.Value().(string), entry.Origin())
		case tag.Date().ID():
			metadata.Add(builder, tag.Date(), entry.Value().(tag.PartialDate), entry.Origin())
		case tag.Comment().ID():
			metadata.Add(builder, tag.Comment(), entry.Value().(string), entry.Origin())
		case tag.Genre().ID():
			metadata.Add(builder, tag.Genre(), entry.Value().(string), entry.Origin())
		case tag.Encoder().ID():
			metadata.Add(builder, tag.Encoder(), entry.Value().(string), entry.Origin())
		case tag.Copyright().ID():
			metadata.Add(builder, tag.Copyright(), entry.Value().(string), entry.Origin())
		}
	}
}

// What a carrier can say is a fact about the carrier, not a mistake by
// whoever asked for it. A key RIFF INFO has no name for used to fail the whole
// write; it is now reported and everything else is still written.
func TestRIFFInfoReportsKeysItHasNoNameForRatherThanRefusing(t *testing.T) {
	resolver := infoTestResolver(t)
	builder := metadata.NewBuilder(metadata.StreamScope)
	document, err := metadata.Add(
		metadata.Add(builder, tag.Title(), "Song", metadata.Origin{}),
		tag.Composer(), "Somebody", metadata.Origin{}).Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, lost, err := resolver.Marshal(t.Context(), RIFFInfo(), "list-0", metadata.MustAvailable(document))
	if err != nil {
		t.Fatalf("marshal refused a document it could partly write: %v", err)
	}
	if !bytes.Contains(encoded.AppendTo(nil), []byte("Song")) {
		t.Fatalf("the writable entry did not survive: %x", encoded.AppendTo(nil))
	}
	if bytes.Contains(encoded.AppendTo(nil), []byte("Somebody")) {
		t.Fatalf("an unrepresentable entry was written anyway: %x", encoded.AppendTo(nil))
	}
	if len(lost) != 1 {
		t.Fatalf("loss report = %#v, want one entry", lost)
	}
	if lost[0].Loss.Key != tag.Composer().ID() || lost[0].Loss.Kind != loss.Dropped {
		t.Fatalf("loss report = %#v", lost[0])
	}
}
