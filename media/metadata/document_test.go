package metadata

import (
	"strings"
	"testing"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/plugin"
)

type otherTestCarrierID struct{}

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
	builder.AddBlock(NewSourceBlock("source", testCarrier, encodingIdentity(), NewBlob("application/octet-stream", []byte{1})))
	origin := func(native string) Origin {
		return Origin{Carrier: testCarrier, Encoding: encodingIdentity(), Block: "source", Native: native}
	}
	Add(builder, title, "First", origin("TIT2"))
	Add(builder, artist, "A", origin("TPE1"))
	Add(builder, artist, "B", origin("TPE1"))
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

func TestBlocksSeparateSourceAnchorsFromOpaquePayload(t *testing.T) {
	payload := NewBlob("application/octet-stream", []byte{1})
	source := NewSourceBlock("source", testCarrier, encodingIdentity(), payload)
	if !source.Valid() || !source.Source() {
		t.Fatalf("source block = %#v", source)
	}
	if NewSourceBlock("source", carrier.ID{}, encodingIdentity(), payload).Valid() {
		t.Fatal("source block without carrier accepted")
	}
	if NewSourceBlock("source", testCarrier, plugin.Identity{}, payload).Valid() {
		t.Fatal("source block without encoding accepted")
	}
	opaque := NewRawBlock("opaque", testCarrier, plugin.Identity{}, payload)
	if !opaque.Valid() || opaque.Source() {
		t.Fatalf("opaque block = %#v", opaque)
	}
	if NewRawBlock("opaque", carrier.ID{}, plugin.Identity{}, payload).Valid() {
		t.Fatal("opaque block without carrier accepted")
	}
}

func TestEntryOriginMustNameMatchingSourceBlock(t *testing.T) {
	payload := NewBlob("application/octet-stream", []byte{1})
	for _, test := range []struct {
		name   string
		block  RawBlock
		origin Origin
		valid  bool
	}{
		{
			name:   "matching source",
			block:  NewSourceBlock("source", testCarrier, encodingIdentity(), payload),
			origin: Origin{Carrier: testCarrier, Encoding: encodingIdentity(), Block: "source"},
			valid:  true,
		},
		{
			name:   "opaque source",
			block:  NewRawBlock("source", testCarrier, encodingIdentity(), payload),
			origin: Origin{Carrier: testCarrier, Encoding: encodingIdentity(), Block: "source"},
		},
		{
			name:   "foreign carrier",
			block:  NewSourceBlock("source", testCarrier, encodingIdentity(), payload),
			origin: Origin{Carrier: carrier.Define[otherTestCarrierID](), Encoding: encodingIdentity(), Block: "source"},
		},
		{
			name:   "foreign encoding",
			block:  NewSourceBlock("source", testCarrier, encodingIdentity(), payload),
			origin: Origin{Carrier: testCarrier, Encoding: otherEncodingIdentity(), Block: "source"},
		},
	} {
		builder := NewBuilder(StreamScope)
		builder.AddBlock(test.block)
		Add(builder, title, "value", test.origin)
		_, err := builder.Build()
		if (err == nil) != test.valid {
			t.Errorf("%s Build error = %v, valid = %v", test.name, err, test.valid)
		}
	}
}

func TestEntryOriginMustBeAbsentOrComplete(t *testing.T) {
	for _, origin := range []Origin{
		{Carrier: testCarrier},
		{Encoding: encodingIdentity()},
		{Native: "TITLE"},
		{Carrier: testCarrier, Encoding: encodingIdentity()},
		{Carrier: testCarrier, Block: "source"},
		{Encoding: encodingIdentity(), Block: "source"},
	} {
		builder := NewBuilder(StreamScope)
		Add(builder, title, "value", origin)
		if _, err := builder.Build(); err == nil {
			t.Fatalf("partial origin accepted: %#v", origin)
		}
	}
	if _, err := Add(NewBuilder(StreamScope), title, "new", Origin{}).Build(); err != nil {
		t.Fatalf("absent origin rejected: %v", err)
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
	for _, want := range []string{"scope", "block", "incomplete source origin"} {
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
	firstBuilder.AddBlock(NewSourceBlock("first", testCarrier, encodingIdentity(), NewBlob("", []byte{1})))
	Add(firstBuilder, title, "Primary", Origin{Carrier: testCarrier, Encoding: encodingIdentity(), Block: "first"})
	first, err := firstBuilder.Build()
	if err != nil {
		t.Fatal(err)
	}
	secondBuilder := NewBuilder(StreamScope)
	secondBuilder.AddBlock(NewSourceBlock("second", testCarrier, encodingIdentity(), NewBlob("", []byte{2})))
	Add(secondBuilder, title, "Alternate", Origin{Carrier: testCarrier, Encoding: encodingIdentity(), Block: "second"})
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
