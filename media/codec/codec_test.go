package codec

import (
	"testing"

	"github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
)

type fixtureCodecID struct{}
type fixtureParserID struct{}
type secondCodecID struct{}

// A codec tag names a codec, not a direction, so each role is its own
// declaration. That is also what lets one tag name several components without
// them colliding: the four decoders of one wire coding are one declaration.
func TestBindingNamesEachRoleSeparately(t *testing.T) {
	tag := format.NewTag("fixture", "tag")
	decoders := BindDecoder(tag, Define[fixtureCodecID](), Define[secondCodecID]())
	encoder := BindEncoder(tag, Define[fixtureCodecID]())
	parser := BindParser(tag, DefineParser[fixtureParserID]())
	if !decoders.Valid() || !encoder.Valid() || !parser.Valid() {
		t.Fatalf("bindings = %#v %#v %#v", decoders, encoder, parser)
	}
	if decoders.Key() == encoder.Key() || decoders.Key() == parser.Key() || encoder.Key() == parser.Key() {
		t.Fatal("two roles of one tag share a declaration key")
	}
	if len(decoders.Targets()) != 2 {
		t.Fatalf("one tag could not name two decoders: %#v", decoders.Targets())
	}
	first, ok := decoders.Targets()[0].Component()
	if !ok || first != plugin.IdentityOf[fixtureCodecID]() {
		t.Fatalf("decoder target = %#v", decoders.Targets())
	}
	for _, testCase := range []struct {
		binding Binding
		role    Role
	}{{decoders, DecoderRole}, {encoder, EncoderRole}, {parser, ParserRole}} {
		got, role, ok := BindingTag(testCase.binding.Key())
		if !ok || got != tag || role != testCase.role {
			t.Fatalf("binding key = %q/%v/%v", got, role, ok)
		}
	}
	if DecoderKey(tag) != decoders.Key() || EncoderKey(tag) != encoder.Key() || ParserKey(tag) != parser.Key() {
		t.Fatal("a role key did not name the declaration its constructor produces")
	}
	if _, _, ok := BindingTag(plugin.Declare[fixtureCodecID]("other", plugin.IdentityOf[fixtureCodecID]()).Key()); ok {
		t.Fatal("a declaration outside the codec namespaces was read as a binding")
	}
}

func TestCodecTagPropertyIsCanonicalAndDeclared(t *testing.T) {
	value := format.NewTag("fixture", "codec")
	properties, err := WithTag(property.New(), value)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := TagOf(properties); !ok || got != value {
		t.Fatalf("codec tag = %q/%v", got, ok)
	}
	if _, err := WithTag(property.New(), ""); err == nil {
		t.Fatal("empty codec tag was accepted")
	}
	declarations := Declarations()
	if len(declarations) != 3 {
		t.Fatalf("codec declarations = %d", len(declarations))
	}
	for _, declaration := range declarations {
		if !declaration.Valid() {
			t.Fatalf("invalid codec declaration: %v", declaration.Problem())
		}
	}
	other, err := WithTag(property.New(), format.NewTag("fixture", "other"))
	if err != nil {
		t.Fatal(err)
	}
	first := stream.MustDescriptor("stream", Packets().Descriptor(), timing.MustBase(1, 48_000), properties)
	second := stream.MustDescriptor("stream", Packets().Descriptor(), timing.MustBase(1, 48_000), other)
	firstFingerprint, err := first.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	secondFingerprint, err := second.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if firstFingerprint == secondFingerprint {
		t.Fatal("codec tag did not participate in descriptor fingerprint")
	}
}
