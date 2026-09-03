package acme

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
)

type foreignMetadataCarrierID struct{}
type foreignMetadataEncodingID struct{}

func TestLabelMarshalReportsEveryUnrepresentableEntryInDocumentOrder(t *testing.T) {
	builder := metadata.NewBuilder(metadata.StreamScope)
	builder.AddBlock(metadata.NewSourceBlock("source", LabelCarrier(), EncodingIdentity(), metadata.NewBlob("application/octet-stream", []byte{1})))
	origin := func(native string) metadata.Origin {
		return metadata.Origin{Carrier: LabelCarrier(), Encoding: EncodingIdentity(), Block: "source", Native: native}
	}
	metadata.Add(builder, tag.Artist(), "dropped first", origin("artist"))
	metadata.Add(builder, Label(), "kept", origin("label"))
	metadata.Add(builder, tag.Title(), "dropped second", origin("title"))
	metadata.Add(builder, Label(), "folded", origin("label"))
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := metadata.NewResolver(map[carrier.ID]plugin.Component{LabelCarrier(): encodingComponent()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	payload, reports, err := resolver.Marshal(t.Context(), LabelCarrier(), "target", metadata.MustAvailable(document))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(payload.AppendTo(nil), []byte("kept")) {
		t.Fatalf("label payload = %q", payload.AppendTo(nil))
	}
	base := loss.Report{Carrier: LabelCarrier(), Encoding: EncodingIdentity().String(), Block: "target"}
	want := []loss.Report{
		{Carrier: base.Carrier, Encoding: base.Encoding, Block: base.Block, Loss: loss.Loss{
			Key: tag.Artist().ID(), Kind: loss.Dropped, Detail: "acme.label-unrepresentable",
			Source: loss.Origin{Carrier: LabelCarrier(), Encoding: EncodingIdentity().String(), Block: "source", Native: "artist"},
		}},
		{Carrier: base.Carrier, Encoding: base.Encoding, Block: base.Block, Loss: loss.Loss{
			Key: tag.Title().ID(), Kind: loss.Dropped, Detail: "acme.label-unrepresentable",
			Source: loss.Origin{Carrier: LabelCarrier(), Encoding: EncodingIdentity().String(), Block: "source", Native: "title"},
		}},
		{Carrier: base.Carrier, Encoding: base.Encoding, Block: base.Block, Loss: loss.Loss{
			Key: Label().ID(), Kind: loss.Folded, Native: "label", Detail: "acme.single-label",
		}},
	}
	if !reflect.DeepEqual(reports, want) {
		t.Fatalf("metadata reports = %#v, want %#v", reports, want)
	}
}

func TestLabelMarshalKeepsInvalidFirstLabelFailure(t *testing.T) {
	builder := metadata.NewBuilder(metadata.StreamScope)
	builder.AddBlock(metadata.NewSourceBlock("source", LabelCarrier(), EncodingIdentity(), metadata.NewBlob("application/octet-stream", []byte{1})))
	origin := metadata.Origin{Carrier: LabelCarrier(), Encoding: EncodingIdentity(), Block: "source", Native: "label"}
	metadata.Add(builder, Label(), "", origin)
	metadata.Add(builder, Label(), "later valid label", origin)
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := metadata.NewResolver(map[carrier.ID]plugin.Component{LabelCarrier(): encodingComponent()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, reports, err := resolver.Marshal(t.Context(), LabelCarrier(), "target", metadata.MustAvailable(document))
	if !errors.Is(err, ErrMalformed) || len(reports) != 0 {
		t.Fatalf("invalid first label = reports %#v, error %v", reports, err)
	}
}

func TestLabelMarshalRejectsForeignOpaqueBlockButAllowsForeignSource(t *testing.T) {
	resolver, err := metadata.NewResolver(map[carrier.ID]plugin.Component{LabelCarrier(): encodingComponent()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	build := func(block metadata.RawBlock) metadata.Document {
		builder := metadata.NewBuilder(metadata.StreamScope)
		builder.AddBlock(metadata.NewSourceBlock("acme/source", LabelCarrier(), EncodingIdentity(), metadata.NewBlob("text/plain", []byte("label"))))
		builder.AddBlock(block)
		metadata.Add(builder, Label(), "label", metadata.Origin{Carrier: LabelCarrier(), Encoding: EncodingIdentity(), Block: "acme/source", Native: "label"})
		document, err := builder.Build()
		if err != nil {
			t.Fatal(err)
		}
		return document
	}
	foreignCarrier := carrier.Define[foreignMetadataCarrierID]()
	foreignEncoding := plugin.IdentityOf[foreignMetadataEncodingID]()
	foreign := metadata.NewRawBlock("foreign/opaque", foreignCarrier, foreignEncoding, metadata.NewBlob("application/octet-stream", []byte("opaque")))
	if _, _, err := resolver.Marshal(t.Context(), LabelCarrier(), "target", metadata.MustAvailable(build(foreign))); !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), string(foreign.ID())) {
		t.Fatalf("foreign opaque metadata error = %v, want ErrUnsupported with block ID", err)
	}
	foreignSource := metadata.NewSourceBlock("foreign/source", foreignCarrier, foreignEncoding, metadata.NewBlob("application/octet-stream", []byte("source")))
	payload, _, err := resolver.Marshal(t.Context(), LabelCarrier(), "target", metadata.MustAvailable(build(foreignSource)))
	if err != nil || !bytes.Equal(payload.AppendTo(nil), []byte("label")) {
		t.Fatalf("foreign source marshal = %q, %v", payload.AppendTo(nil), err)
	}
}

func TestWriterRejectsUnavailableMetadataWithoutMarshal(t *testing.T) {
	marshalCalls := 0
	encoding := plugin.NewComponent[encodingID](plugin.Descriptor{DisplayName: "counting ACME label encoding"}, configurationSchema(),
		metadata.WithEncoding(parseLabel, func(ctx metadata.MarshalContext) (metadata.Blob, []loss.Loss, error) {
			marshalCalls++
			return marshalLabel(ctx)
		}, Label().Erased()))
	resolver, err := metadata.NewResolver(map[carrier.ID]plugin.Component{LabelCarrier(): encoding}, nil)
	if err != nil {
		t.Fatal(err)
	}
	context, err := metadata.WithResolver(plugin.CompileContextWithContext(plugin.CompileContext{}, t.Context()), resolver)
	if err != nil {
		t.Fatal(err)
	}
	input := stream.MustDescriptor("values", Values().Descriptor(), timing.Base{}, property.New()).WithMetadata(metadata.MustUnavailable(metadata.StreamScope))
	component := writerComponent()
	resolved, err := component.Resolve(config.NewPatch())
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := plugin.Compile(component, context, resolved, flow.NewDescriptors(flow.Describe("values", input)))
	if err == nil {
		t.Fatal("unavailable metadata unexpectedly compiled")
	}
	items := diagnostic.ItemsOf(err)
	if len(items) != 1 || !strings.Contains(items[0].Detail["cause"], "metadata.unavailable") || !strings.Contains(items[0].Detail["cause"], metadata.ErrMetadataUnavailable.Error()) {
		t.Fatalf("unavailable writer diagnostic = %#v/%v", items, err)
	}
	if marshalCalls != 0 {
		t.Fatalf("unavailable writer marshal calls = %d, want 0", marshalCalls)
	}
	if len(compiled.MetadataReports()) != 0 {
		t.Fatalf("failed writer metadata reports = %#v", compiled.MetadataReports())
	}
}
