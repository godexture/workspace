package id3

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/plugin"
)

type v2TestCarrierID struct{}

func TestV2ParseAndUnchangedMarshalPreserveSourceBytes(t *testing.T) {
	payload := v2TestTag(v2BuildFrame("TIT2", []byte{3, 'T', 'i', 't', 'l', 'e'}))
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	document, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, payload))
	if err != nil {
		t.Fatal(err)
	}
	title, ok := metadata.First(document, tag.Title())
	if !ok || title != "Title" {
		t.Fatalf("ID3v2 title = %q/%v", title, ok)
	}
	block, ok := document.Block("head")
	if !ok || !block.Source() || !bytes.Equal(block.Payload().AppendTo(nil), payload) {
		t.Fatalf("ID3v2 source block = %#v/%v", block, ok)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "head", document)
	if err != nil || len(reports) != 0 || !bytes.Equal(encoded.AppendTo(nil), payload) {
		t.Fatalf("ID3v2 unchanged = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}
}

func TestV2FreshTitleWritesV24UTF8(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	builder := metadata.NewBuilder(metadata.StreamScope)
	metadata.Add(builder, tag.Title(), "新しい題", metadata.Origin{})
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "head", document)
	if err != nil || len(reports) != 0 {
		t.Fatalf("fresh ID3v2 = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}
	value := encoded.AppendTo(nil)
	if len(value) < 10 || !bytes.Equal(value[:6], []byte{'I', 'D', '3', 4, 0, 0}) || !bytes.Contains(value[10:], append([]byte{3}, []byte("新しい題")...)) {
		t.Fatalf("fresh ID3v2 bytes = %x", value)
	}
}

func TestV2SyncSafeEncoding(t *testing.T) {
	for _, test := range []struct {
		value int
		want  []byte
	}{
		{value: 0, want: []byte{0, 0, 0, 0}},
		{value: 127, want: []byte{0, 0, 0, 127}},
		{value: 128, want: []byte{0, 0, 1, 0}},
		{value: 16383, want: []byte{0, 0, 127, 127}},
		{value: 16384, want: []byte{0, 1, 0, 0}},
		{value: 65535, want: []byte{0, 3, 127, 127}},
		{value: 0x0fffffff, want: []byte{127, 127, 127, 127}},
	} {
		got := v2EncodeSyncSafe(test.value)
		if !bytes.Equal(got, test.want) {
			t.Fatalf("syncsafe(%d) = %x, want %x", test.value, got, test.want)
		}
		decoded, ok := v2DecodeSyncSafe(got)
		if !ok || decoded != test.value {
			t.Fatalf("syncsafe roundtrip(%d) = %d/%v", test.value, decoded, ok)
		}
	}
}

func FuzzV2UnchangedDocumentsReturnExactSource(f *testing.F) {
	for _, value := range [][]byte{
		v2TestTag(v2BuildFrame("TIT2", []byte{3, 'T'})),
		v2TestTag(v2BuildFrame("TIT2", []byte{3, 'A'}), v2BuildFrame("XAAA", []byte{1, 2})),
		v2TestTagVersion(2, 0, v2TestFrame(2, "PIC", []byte{0, 'P', 'N', 'G', byte(tag.ArtworkFrontCover), 0, 1}, [2]byte{})),
	} {
		f.Add(value)
	}
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(f, slot)
	f.Fuzz(func(t *testing.T, value []byte) {
		payload := metadata.NewBlob(v2MediaType, value)
		document, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, payload)
		if err != nil {
			return
		}
		encoded, reports, err := resolver.Marshal(t.Context(), slot, "head", document)
		if err != nil || len(reports) != 0 || !bytes.Equal(encoded.AppendTo(nil), value) {
			t.Fatalf("roundtrip = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
		}
	})
}

func v2TestResolver(t testing.TB, slot carrier.ID) metadata.Resolver {
	t.Helper()
	resolver, err := metadata.NewResolver(map[carrier.ID]plugin.Component{slot: v2Component()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return resolver
}

func v2TestTag(frames ...[]byte) []byte {
	return v2TestTagVersion(4, 0, frames...)
}

func v2TestTagVersion(version, flags byte, frames ...[]byte) []byte {
	payload := make([]byte, 0)
	for _, frame := range frames {
		payload = append(payload, frame...)
	}
	value := []byte{'I', 'D', '3', version, 0, flags}
	value = append(value, v2EncodeSyncSafe(len(payload))...)
	return append(value, payload...)
}

func v2TestFrame(version byte, identity string, payload []byte, flags [2]byte) []byte {
	if version == 2 {
		frame := append([]byte(identity), byte(len(payload)>>16), byte(len(payload)>>8), byte(len(payload)))
		return append(frame, payload...)
	}
	frame := append([]byte(identity), make([]byte, 4)...)
	if version == 3 {
		binary.BigEndian.PutUint32(frame[4:8], uint32(len(payload)))
	} else {
		copy(frame[4:8], v2EncodeSyncSafe(len(payload)))
	}
	frame = append(frame, flags[0], flags[1])
	return append(frame, payload...)
}
