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

func TestBindingKeepsParserIndependentFromFormat(t *testing.T) {
	parser := DefineParser[fixtureParserID]()
	binding := Bind(format.NewTag("fixture", "tag"), Define[fixtureCodecID](), parser)
	targets := binding.Targets()
	if !binding.Valid() || len(targets) != 2 {
		t.Fatalf("binding = %#v", binding)
	}
	codec, codecTarget := targets[0].Component()
	if !codecTarget || codec != plugin.IdentityOf[fixtureCodecID]() {
		t.Fatalf("codec target = %#v", targets)
	}
	parserIdentity, parserTarget := targets[1].Component()
	if !parserTarget || parserIdentity != plugin.IdentityOf[fixtureParserID]() {
		t.Fatalf("parser target = %#v", targets)
	}
	if got, ok := BindingTag(binding.Key()); !ok || got != format.NewTag("fixture", "tag") || BindingKey(got) != binding.Key() || !IsBindingKey(binding.Key()) {
		t.Fatalf("binding key = %q/%v", got, ok)
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
	if len(declarations) != 2 {
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
