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
	if !binding.Valid() || len(binding.Targets()) != 2 || binding.Targets()[0] != plugin.IdentityOf[fixtureCodecID]() {
		t.Fatalf("binding = %#v", binding)
	}
	if binding.Targets()[1] != plugin.IdentityOf[fixtureParserID]() {
		t.Fatalf("parser target = %#v", binding.Targets())
	}
}
