package standard_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/host"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/codec"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/plugin/file"
	"github.com/godexture/godec/plugin/mp4"
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

	if first.Catalog().Len() != 12 {
		t.Fatalf("catalog components = %d, want 12", first.Catalog().Len())
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
		mp4.DemuxerIdentity(),
		mp4.MuxerIdentity(),
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

func TestNewFileJobCarriesCanonicalExtensionSelectorsWithoutIO(t *testing.T) {
	request, err := standard.NewFileJob(filepath.Join("missing", "Input.WAV"), filepath.Join("missing", "Output.RAW"))
	if err != nil {
		t.Fatal(err)
	}
	hint, ok := request.Inputs()[0].FormatHint()
	if !ok {
		t.Fatal("input extension hint is absent")
	}
	inputExtension, ok := hint.Extension()
	if !ok || inputExtension.String() != "wav" {
		t.Fatalf("input extension = %q/%v", inputExtension, ok)
	}
	selection, ok := request.Outputs()[0].FormatRequest()
	if !ok {
		t.Fatal("output extension request is absent")
	}
	outputExtension, ok := selection.Extension()
	if !ok || outputExtension.String() != "raw" {
		t.Fatalf("output extension = %q/%v", outputExtension, ok)
	}
	if _, err := standard.NewFileJob("", "output.wav"); err == nil {
		t.Fatal("empty input path accepted")
	}
}

func TestNewFileJobAllowsExplicitFormatAndExtensionlessOutput(t *testing.T) {
	selector, err := job.SelectFormat(linear.Raw())
	if err != nil {
		t.Fatal(err)
	}
	selector = selector.WithConfig(config.NewPatch().Set(readerKey(t, "rate"), 48_000))
	request, err := standard.NewFileJob("input.wav", "output", standard.WithInputFormat(selector))
	if err != nil {
		t.Fatal(err)
	}
	hint, ok := request.Inputs()[0].FormatHint()
	if !ok {
		t.Fatal("explicit input selector is absent")
	}
	identity, ok := hint.Identity()
	if !ok || identity != linear.Raw().Identity() {
		t.Fatalf("explicit Format = %v/%v", identity, ok)
	}
	patch, ok := hint.Config()
	if !ok || !slices.Equal(patch.FieldIDs(), []string{"rate"}) {
		t.Fatalf("explicit config = %v/%v", patch.FieldIDs(), ok)
	}
	if _, ok := request.Outputs()[0].FormatRequest(); ok {
		t.Fatal("extensionless output unexpectedly has a Format request")
	}
	if _, err := standard.NewFileJob("input.wav", "output.wav", standard.WithOutputFormat(job.FormatSelector{})); err == nil {
		t.Fatal("invalid explicit output selector accepted")
	}
}

func TestNewFileJobForwardsPolicyAndBudget(t *testing.T) {
	policy, ok := job.PolicyFor(job.Realtime)
	if !ok {
		t.Fatal("realtime policy is unavailable")
	}
	budget := job.DefaultBudget()
	budget.States--
	request, err := standard.NewFileJob(
		"input.wav",
		"output.wav",
		standard.WithPolicy(policy),
		standard.WithBudget(budget),
	)
	if err != nil {
		t.Fatal(err)
	}
	if request.Policy() != policy || request.Budget() != budget {
		t.Fatalf("file job policy/budget = %#v, %#v", request.Policy(), request.Budget())
	}
}

type extraTraitKey struct{}

func readerKey(t *testing.T, field string) config.Key {
	t.Helper()
	for _, component := range standard.Set().Components() {
		if component.Identity() != linear.ReaderIdentity() {
			continue
		}
		key, ok := component.Schema().Key(field)
		if !ok {
			t.Fatalf("linear reader schema has no %s field", field)
		}
		return key
	}
	t.Fatal("standard composition has no linear reader component")
	return config.Key{}
}
