package mp4

import (
	"testing"

	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/tag"
)

func TestMP4Definition(t *testing.T) {
	value := MP4()
	if !value.Valid() || !value.Packetized() {
		t.Fatalf("MP4() = %#v, want valid packetized Format", value)
	}
	extensions := value.Extensions()
	if len(extensions) != 1 || extensions[0].String() != "mp4" {
		t.Fatalf("MP4 extensions = %v, want [mp4]", extensions)
	}
	if !value.Same(MP4()) {
		t.Fatal("MP4 declaration is not stable")
	}
}

func TestPluginIncludesStandaloneIlstEncoding(t *testing.T) {
	for _, component := range Plugin().Components() {
		if component.Identity() != IlstEncodingIdentity() {
			continue
		}
		encoding, ok := metadata.EncodingOf(component)
		if !ok || !encoding.Valid() || !encoding.Supports(tag.Title().ID()) {
			t.Fatalf("ilst encoding = %#v/%v", encoding, ok)
		}
		return
	}
	t.Fatal("MP4 plugin has no ilst encoding component")
}
