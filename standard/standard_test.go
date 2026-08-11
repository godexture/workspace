package standard_test

import (
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/host"
	"github.com/godexture/godec/media/codec"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/plugin/file"
	"github.com/godexture/godec/plugin/pcm/linear"
	"github.com/godexture/godec/plugin/wave"
	"github.com/godexture/godec/standard"
)

func TestSetBuildsCompleteDeterministicCatalog(t *testing.T) {
	first, err := host.New(host.Plugins(standard.Set()))
	if err != nil {
		t.Fatal(err)
	}
	second, err := standard.NewHost()
	if err != nil {
		t.Fatal(err)
	}

	if first.Catalog().Len() != 10 {
		t.Fatalf("catalog components = %d, want 10", first.Catalog().Len())
	}
	if first.Catalog().Fingerprint() != second.Catalog().Fingerprint() {
		t.Fatal("equivalent standard compositions have different fingerprints")
	}
	for _, identity := range []plugin.Identity{
		file.SourceIdentity(),
		file.SinkIdentity(),
		linear.ReaderIdentity(),
		linear.ParserIdentity(),
		linear.DecoderIdentity(),
		linear.EncoderIdentity(),
		linear.WriterIdentity(),
		wave.DemuxerIdentity(),
		wave.MuxerIdentity(),
		wave.InfoEncodingIdentity(),
	} {
		if _, ok := first.Catalog().Lookup(identity); !ok {
			t.Fatalf("standard catalog does not contain %s", identity)
		}
	}
}

type extraPluginID struct{}
type extraComponentID struct{}
type extraConfigID struct{}

type extraConfig struct{}

func TestNewHostAddsDefinitionThroughTheSameComposition(t *testing.T) {
	schema := config.Struct[extraConfigID](func() extraConfig { return extraConfig{} }).Version("1").Build()
	tag := mediaformat.NewTag("extra", "linear")
	extra := plugin.Define[extraPluginID](plugin.Descriptor{
		DisplayName: "Extra",
		Version:     "1.0.0",
		License:     "MIT",
		Build:       plugin.BuildModePureGo,
	}, plugin.NewComponent[extraComponentID](plugin.Descriptor{DisplayName: "Extra trait"}, schema,
		plugin.WithTrait(plugin.TraitKeyOf[extraTraitKey](), "extra=true", plugin.PortShapeOptional, struct{}{}),
	)).WithDeclarations(codec.BindWithoutParser(tag, codec.New(linear.DecoderIdentity())))

	instance, err := standard.NewHost(extra)
	if err != nil {
		t.Fatal(err)
	}
	view, ok := instance.Catalog().Lookup(plugin.IdentityOf[extraComponentID]())
	if !ok {
		t.Fatal("extra definition was not added")
	}
	if view.Executable {
		t.Fatal("trait-only extra component is executable")
	}
	foundBinding := false
	for _, declaration := range instance.Catalog().Declarations() {
		if declaration.Key() == codec.BindingKey(tag) {
			foundBinding = declaration.Owner() == extra.Identity()
		}
	}
	if !foundBinding {
		t.Fatal("extra definition did not carry its owned binding")
	}
}

type extraTraitKey struct{}
