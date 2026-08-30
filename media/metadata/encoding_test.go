package metadata

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/plugin"
)

type encodingTraitComponentID struct{}
type encodingTraitConfigID struct{}
type encodingTraitCarrierID struct{}
type encodingTraitKeyID struct{}
type encodingTraitOtherKeyID struct{}

var encodingTraitDeclaration = key.Define[encodingTraitKeyID, string]()
var encodingTraitOtherDeclaration = key.Define[encodingTraitOtherKeyID, string]()

func TestEncodingTraitIsPureControlPlaneBehavior(t *testing.T) {
	slot := carrier.Define[encodingTraitCarrierID]()
	parseCalls := 0
	marshalCalls := 0
	component := encodingTraitComponent(
		func(ctx ParseContext) (Document, error) {
			parseCalls++
			builder := NewBuilder(ctx.Scope())
			builder.AddBlock(NewSourceBlock(ctx.Block(), ctx.Carrier(), ctx.Encoding(), ctx.Payload()))
			return builder.Build()
		},
		func(ctx MarshalContext) (Blob, []loss.Loss, error) {
			marshalCalls++
			block, ok := ctx.Document().Block(ctx.Block())
			if !ok || block.Carrier() != ctx.Carrier() || block.Encoding() != ctx.Encoding() {
				return Blob{}, nil, errors.New("raw block does not belong to encoding")
			}
			return block.Payload(), nil, nil
		},
	)
	if items := component.Diagnostics(); len(items) != 0 {
		t.Fatalf("encoding component diagnostics = %v", items)
	}
	view := component.View()
	if view.HasSpec || view.Executable || !view.Ports.Empty() || len(view.Traits) != 1 || view.Traits[0].ShapeRequirement != plugin.PortShapeOptional {
		t.Fatalf("encoding component view = %#v", view)
	}
	encoding, ok := EncodingOf(component)
	if !ok || !encoding.Valid() {
		t.Fatalf("EncodingOf = %#v/%v", encoding, ok)
	}

	resolver, err := NewResolver(map[carrier.ID]plugin.Component{slot: component}, nil)
	if err != nil {
		t.Fatal(err)
	}
	payload := NewBlob("application/octet-stream", []byte{1, 2, 3})
	first, err := resolver.Parse(t.Context(), slot, "block", StreamScope, payload)
	if err != nil {
		t.Fatal(err)
	}
	second, err := resolver.Parse(t.Context(), slot, "block", StreamScope, payload)
	if err != nil {
		t.Fatal(err)
	}
	if first.Scope() != StreamScope || second.Scope() != StreamScope || first.Len() != second.Len() || parseCalls != 2 {
		t.Fatalf("deterministic Parse = %#v/%#v calls=%d", first, second, parseCalls)
	}
	marshalled, _, err := resolver.Marshal(t.Context(), slot, "block", first)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(marshalled.AppendTo(nil), []byte{1, 2, 3}) || marshalCalls != 1 {
		t.Fatalf("Marshal = %v calls=%d", marshalled.AppendTo(nil), marshalCalls)
	}

	compileContext, err := WithResolver(plugin.CompileContext{}, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if recovered, ok := ResolverOf(compileContext); !ok || !recovered.Valid() {
		t.Fatalf("ResolverOf = %#v/%v", recovered, ok)
	}
	if _, err := WithResolver(compileContext, resolver); !errors.Is(err, plugin.ErrDuplicateTrait) {
		t.Fatalf("duplicate resolver error = %v", err)
	}
}

func TestResolverReportsBindingAndEncodingFailures(t *testing.T) {
	slot := carrier.Define[encodingTraitCarrierID]()
	empty, err := NewResolver(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := empty.Parse(t.Context(), slot, "block", StreamScope, NewBlob("", nil)); !hasMetadataDiagnostic(err, "metadata.binding-missing") {
		t.Fatalf("missing binding diagnostic = %v", err)
	}

	parseFailure := errors.New("parse failed")
	component := encodingTraitComponent(
		func(ParseContext) (Document, error) { return Document{}, parseFailure },
		func(MarshalContext) (Blob, []loss.Loss, error) { return Blob{}, nil, errors.New("marshal failed") },
	)
	resolver, err := NewResolver(map[carrier.ID]plugin.Component{slot: component}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Parse(t.Context(), slot, "block", StreamScope, NewBlob("", nil)); !hasMetadataDiagnostic(err, "metadata.parse") || !errors.Is(err, parseFailure) {
		t.Fatalf("parse diagnostic = %v", err)
	}
	document, err := NewBuilder(StreamScope).Build()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := resolver.Marshal(t.Context(), slot, "block", document); !hasMetadataDiagnostic(err, "metadata.marshal") {
		t.Fatalf("marshal diagnostic = %v", err)
	}

	invalid := encodingTraitComponent(nil, nil)
	if value, ok := EncodingOf(invalid); !ok || value.Valid() {
		t.Fatalf("invalid EncodingOf = %#v/%v", value, ok)
	}
	if _, err := NewResolver(map[carrier.ID]plugin.Component{slot: invalid}, nil); !errors.Is(err, ErrInvalidResolver) {
		t.Fatalf("invalid resolver error = %v", err)
	}
}

func TestEncodingDeclaresImmutableDirectKeySet(t *testing.T) {
	component := plugin.NewComponent[encodingTraitComponentID](plugin.Descriptor{DisplayName: "encoding"}, config.Struct[encodingTraitConfigID](func() struct{} { return struct{}{} }).Version("1").Build(),
		WithEncoding(
			func(ctx ParseContext) (Document, error) { return NewBuilder(ctx.Scope()).Build() },
			func(MarshalContext) (Blob, []loss.Loss, error) {
				return NewBlob("application/octet-stream", nil), nil, nil
			},
			encodingTraitOtherDeclaration.Erased(), encodingTraitDeclaration.Erased(),
		),
	)
	encoding, ok := EncodingOf(component)
	if !ok || !encoding.Valid() || !encoding.Supports(encodingTraitDeclaration.ID()) || !encoding.Supports(encodingTraitOtherDeclaration.ID()) || encoding.Supports(title.ID()) {
		t.Fatalf("encoding support = %#v/%v", encoding, ok)
	}
	encoding.supported[0] = title.ID()
	recovered, _ := EncodingOf(component)
	if recovered.Supports(title.ID()) || !recovered.Supports(encodingTraitDeclaration.ID()) {
		t.Fatal("EncodingOf exposed the trait key set")
	}
	traits := component.Traits()
	if len(traits) != 1 || !strings.Contains(traits[0].Manifest, "keys="+encodingTraitDeclaration.ID().String()+","+encodingTraitOtherDeclaration.ID().String()) {
		t.Fatalf("encoding manifest = %#v", traits)
	}
	parse := func(ctx ParseContext) (Document, error) { return NewBuilder(ctx.Scope()).Build() }
	marshal := func(MarshalContext) (Blob, []loss.Loss, error) {
		return NewBlob("application/octet-stream", nil), nil, nil
	}
	if newEncoding(parse, marshal, nil).Valid() || newEncoding(parse, marshal, []key.Erased{encodingTraitDeclaration.Erased(), encodingTraitDeclaration.Erased()}).Valid() {
		t.Fatal("empty or duplicate direct key set is valid")
	}
}

func TestInvalidEncodingExplainsItsDeclarationProblem(t *testing.T) {
	parse := func(ctx ParseContext) (Document, error) { return NewBuilder(ctx.Scope()).Build() }
	marshal := func(MarshalContext) (Blob, []loss.Loss, error) {
		return NewBlob("application/octet-stream", nil), nil, nil
	}
	for _, test := range []struct {
		name  string
		value Encoding
		want  string
	}{
		{name: "parse", value: newEncoding(nil, marshal, []key.Erased{encodingTraitDeclaration.Erased()}), want: "requires a Parse function"},
		{name: "marshal", value: newEncoding(parse, nil, []key.Erased{encodingTraitDeclaration.Erased()}), want: "requires a Marshal function"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.value.Valid() || test.value.Problem() == nil || !strings.Contains(test.value.Problem().Error(), test.want) {
				t.Fatalf("encoding problem = %v", test.value.Problem())
			}
			if manifest := test.value.manifest(); !strings.HasPrefix(manifest, "invalid=") || strings.Contains(manifest, "parse=true") || strings.Contains(manifest, "marshal=true") {
				t.Fatalf("invalid encoding manifest = %q", manifest)
			}
		})
	}
}

func encodingTraitComponent(parse ParseFunc, marshal MarshalFunc) plugin.Component {
	schema := config.Struct[encodingTraitConfigID](func() struct{} { return struct{}{} }).Version("1").Build()
	return plugin.NewComponent[encodingTraitComponentID](plugin.Descriptor{DisplayName: "encoding"}, schema, WithEncoding(parse, marshal, encodingTraitDeclaration.Erased()))
}

func hasMetadataDiagnostic(err error, code string) bool {
	for _, item := range diagnostic.ItemsOf(err) {
		if item.Code == code {
			return true
		}
	}
	return false
}
