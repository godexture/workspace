package codec

import (
	"testing"

	"github.com/godexture/godec/media/format"
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
}
