package metadata

import (
	"strings"
	"testing"

	"github.com/godexture/godec/media/carrier"
)

func TestOriginLossOriginRequiresCompleteSource(t *testing.T) {
	complete := Origin{Carrier: testCarrier, Encoding: encodingIdentity(), Block: "source", Native: "native"}
	if got := complete.LossOrigin(); !got.Valid() || got.Carrier != complete.Carrier || got.Encoding != complete.Encoding.String() || got.Block != string(complete.Block) || got.Native != complete.Native {
		t.Fatalf("complete loss origin = %#v", got)
	}
	if got := (Origin{Carrier: testCarrier, Encoding: encodingIdentity()}).LossOrigin(); !got.IsZero() {
		t.Fatalf("partial loss origin = %#v", got)
	}
}

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
	if values := Values(document, artist); len(values) != 2 || values[0] != "A" || values[1] != "B" {
		t.Fatalf("duplicate key values = %v", values)
	}
	entries := document.Entries()
	if entries[0].Key() != title.ID() || entries[1].Key() != artist.ID() {
		t.Fatalf("entry order = %v, %v", entries[0].Key(), entries[1].Key())
	}
	if entries[0].Origin().Native != "TIT2" || entries[0].Origin().Encoding != encodingIdentity() {
		t.Fatalf("origin = %#v", entries[0].Origin())
	}
	if value, ok := First(document, title); !ok || value != "First" {
		t.Fatalf("first title = %q, %v", value, ok)
	}
}

func TestDocumentCannotBeChangedThroughTheSlicesItReturns(t *testing.T) {
	builder := NewBuilder(AssetScope)
	Add(builder, title, "Original", Origin{})
	builder.AddBlock(NewRawBlock("block-1", testCarrier, encodingIdentity(), NewBlob("application/octet-stream", []byte{1, 2})))
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	entries := document.Entries()
	entries[0] = Entry{}
	blocks := document.Blocks()
	blocks[0] = RawBlock{}
	if value, ok := First(document, title); !ok || value != "Original" {
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
	block := NewRawBlock("unknown-frame", testCarrier, otherEncodingIdentity(), NewBlob("", payload))
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
	if stored.Encoding() != otherEncodingIdentity() || stored.Carrier() != testCarrier {
		t.Fatalf("raw block provenance = %#v", stored)
	}
}

func TestBuildReportsEveryProblemAtOnce(t *testing.T) {
	builder := NewBuilder(Scope(0))
	builder.AddBlock(NewRawBlock("", carrier.ID{}, encodingIdentity(), NewBlob("", nil)))
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
	block := NewRawBlock("same", testCarrier, encodingIdentity(), NewBlob("", []byte{1}))
	builder.AddBlock(block).AddBlock(block)
	if _, err := builder.Build(); err == nil {
		t.Fatal("repeated raw block identity accepted")
	}
}

func TestBuilderAppendPreservesDocumentAndRawBlockOrder(t *testing.T) {
	firstBuilder := NewBuilder(StreamScope)
	firstBuilder.AddBlock(NewRawBlock("first", testCarrier, encodingIdentity(), NewBlob("", []byte{1})))
	Add(firstBuilder, title, "Primary", Origin{Block: "first"})
	first, err := firstBuilder.Build()
	if err != nil {
		t.Fatal(err)
	}
	secondBuilder := NewBuilder(StreamScope)
	secondBuilder.AddBlock(NewRawBlock("second", testCarrier, encodingIdentity(), NewBlob("", []byte{2})))
	Add(secondBuilder, title, "Alternate", Origin{Block: "second"})
	second, err := secondBuilder.Build()
	if err != nil {
		t.Fatal(err)
	}

	merged, err := NewBuilder(StreamScope).Append(Document{}).Append(first).Append(second).Build()
	if err != nil {
		t.Fatal(err)
	}
	if values := Values(merged, title); len(values) != 2 || values[0] != "Primary" || values[1] != "Alternate" {
		t.Fatalf("merged entry order = %v", values)
	}
	blocks := merged.Blocks()
	if len(blocks) != 2 || blocks[0].ID() != "first" || blocks[1].ID() != "second" {
		t.Fatalf("merged block order = %#v", blocks)
	}
}

func TestBuilderAppendRejectsScopeAndBlockConflicts(t *testing.T) {
	asset, err := NewBuilder(AssetScope).Build()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewBuilder(StreamScope).Append(asset).Build(); err == nil {
		t.Fatal("Builder.Append accepted a different document scope")
	}

	block := NewRawBlock("same", testCarrier, encodingIdentity(), NewBlob("", nil))
	first, err := NewBuilder(StreamScope).AddBlock(block).Build()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewBuilder(StreamScope).Append(first).Append(first).Build(); err == nil {
		t.Fatal("Builder.Append accepted a duplicate raw block identity")
	}
}
