package metadata

import (
	"strings"
	"testing"

	"github.com/godexture/godec/media/format"
)

func TestDocumentKeepsOrderDuplicateKeysAndOrigin(t *testing.T) {
	builder := NewBuilder(StreamScope)
	Add(builder, title, "First", Origin{Encoding: encodingIdentity(), Native: "TIT2"})
	Add(builder, artist, "A", Origin{Encoding: encodingIdentity(), Native: "TPE1"})
	Add(builder, artist, "B", Origin{Encoding: encodingIdentity(), Native: "TPE1"})
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	if document.Scope() != StreamScope || document.Len() != 3 {
		t.Fatalf("document scope = %v, len = %d", document.Scope(), document.Len())
	}
	if values := artist.Values(document); len(values) != 2 || values[0] != "A" || values[1] != "B" {
		t.Fatalf("duplicate key values = %v", values)
	}
	entries := document.Entries()
	if entries[0].Key() != title.ID() || entries[1].Key() != artist.ID() {
		t.Fatalf("entry order = %v, %v", entries[0].Key(), entries[1].Key())
	}
	if entries[0].Origin().Native != "TIT2" || entries[0].Origin().Encoding != encodingIdentity() {
		t.Fatalf("origin = %#v", entries[0].Origin())
	}
	if value, ok := title.First(document); !ok || value != "First" {
		t.Fatalf("first title = %q, %v", value, ok)
	}
}

func TestDocumentCannotBeChangedThroughTheSlicesItReturns(t *testing.T) {
	builder := NewBuilder(AssetScope)
	Add(builder, title, "Original", Origin{})
	builder.AddBlock(NewRawBlock("block-1", format.CarrierID("wave.id3"), encodingIdentity(), NewBlob("application/octet-stream", []byte{1, 2})))
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	entries := document.Entries()
	entries[0] = Entry{}
	blocks := document.Blocks()
	blocks[0] = RawBlock{}
	if value, ok := title.First(document); !ok || value != "Original" {
		t.Fatalf("entry survived through returned slice: %q, %v", value, ok)
	}
	if _, ok := document.Block("block-1"); !ok {
		t.Fatal("raw block survived through returned slice")
	}
}

func TestEditProducesANewDocumentAndLeavesTheOriginal(t *testing.T) {
	builder := NewBuilder(AssetScope)
	Add(builder, title, "Original", Origin{})
	original, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	edited, err := Add(original.Edit(), artist, "Added", Origin{}).Build()
	if err != nil {
		t.Fatal(err)
	}
	if original.Len() != 1 || edited.Len() != 2 {
		t.Fatalf("original = %d, edited = %d", original.Len(), edited.Len())
	}
}

func TestRawBlockKeepsUninterpretedPayloadForLosslessRewrite(t *testing.T) {
	payload := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	block := NewRawBlock("unknown-frame", format.CarrierID("mp3.leading"), otherEncodingIdentity(), NewBlob("", payload))
	document, err := NewBuilder(AssetScope).AddBlock(block).Build()
	if err != nil {
		t.Fatal(err)
	}
	stored, ok := document.Block("unknown-frame")
	if !ok {
		t.Fatal("raw block was not preserved")
	}
	payload[0] = 0
	if got := stored.Payload().AppendTo(nil); string(got) != string([]byte{0xDE, 0xAD, 0xBE, 0xEF}) {
		t.Fatalf("raw payload = %v", got)
	}
	if stored.Encoding() != otherEncodingIdentity() || stored.Carrier() != format.CarrierID("mp3.leading") {
		t.Fatalf("raw block provenance = %#v", stored)
	}
}

func TestBuildReportsEveryProblemAtOnce(t *testing.T) {
	builder := NewBuilder(Scope(0))
	builder.AddBlock(NewRawBlock("", "", encodingIdentity(), NewBlob("", nil)))
	Add(builder, title, "value", Origin{Block: "missing"})
	_, err := builder.Build()
	if err == nil {
		t.Fatal("invalid document accepted")
	}
	message := err.Error()
	for _, want := range []string{"scope", "raw block", "missing"} {
		if !strings.Contains(message, want) {
			t.Fatalf("aggregate error %q does not mention %q", message, want)
		}
	}
}

func TestDuplicateRawBlockIdentityIsRejected(t *testing.T) {
	builder := NewBuilder(AssetScope)
	block := NewRawBlock("same", "carrier", encodingIdentity(), NewBlob("", []byte{1}))
	builder.AddBlock(block).AddBlock(block)
	if _, err := builder.Build(); err == nil {
		t.Fatal("repeated raw block identity accepted")
	}
}
