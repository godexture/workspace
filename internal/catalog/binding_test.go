package catalog

import (
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/codec"
	"github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/plugin"
)

type bindingPluginID struct{}
type secondBindingPluginID struct{}
type bindingComponentID struct{}
type secondBindingComponentID struct{}
type metadataBindingComponentID struct{}
type metadataBindingCarrierID struct{}
type metadataBindingConfigID struct{}

func TestBuildRejectsConflictingCodecBindings(t *testing.T) {
	definition := plugin.Define[bindingPluginID](plugin.Descriptor{DisplayName: "binding plugin", Version: "1"}, catalogComponent[bindingComponentID]("binding"))
	key := format.NewTag("fixture", "tag")
	first := codec.BindDecoder(key, codec.New(plugin.IdentityOf[bindingComponentID]()))
	second := codec.BindDecoder(key, codec.New(plugin.IdentityOf[secondBindingComponentID]()))
	_, err := Build(plugin.NewSet(definition).AddDeclaration(first).AddDeclaration(second))
	if err == nil {
		t.Fatal("conflicting binding unexpectedly accepted")
	}
	for _, item := range diagnosticItems(err) {
		if item.Code == "catalog.declaration-conflict" {
			return
		}
	}
	t.Fatalf("binding conflict diagnostic missing: %v", err)
}

func TestBuildRejectsDeclarationWithMissingTarget(t *testing.T) {
	definition := plugin.Define[bindingPluginID](plugin.Descriptor{DisplayName: "binding plugin", Version: "1"}, catalogComponent[bindingComponentID]("binding"))
	missing := codec.BindDecoder(format.NewTag("fixture", "missing"), codec.New(plugin.IdentityOf[secondBindingComponentID]()))
	_, err := Build(plugin.NewSet(definition).AddDeclaration(missing))
	if err == nil {
		t.Fatal("declaration with missing target unexpectedly accepted")
	}
	for _, item := range diagnosticItems(err) {
		if item.Code == "catalog.declaration-target" && item.Path.Component == plugin.IdentityOf[secondBindingComponentID]().String() {
			return
		}
	}
	t.Fatalf("missing declaration target diagnostic absent: %v", err)
}

func TestOwnedDeclarationMayTargetAnotherDefinition(t *testing.T) {
	ownerComponent := catalogComponent[bindingComponentID]("owner")
	targetComponent := catalogComponent[secondBindingComponentID]("target")
	binding := codec.BindDecoder(format.NewTag("fixture", "external"), codec.New(targetComponent.Identity()))
	owner := plugin.Define[bindingPluginID](plugin.Descriptor{DisplayName: "owner plugin", Version: "1"}, ownerComponent).
		WithDeclarations(binding)
	target := plugin.Define[secondBindingPluginID](plugin.Descriptor{DisplayName: "target plugin", Version: "1"}, targetComponent)

	if _, err := Build(plugin.NewSet(owner, target)); err != nil {
		t.Fatalf("external owned declaration target rejected: %v", err)
	}
}

func TestRemovingOwnedDeclarationTargetReportsTargetAndOwner(t *testing.T) {
	target := catalogComponent[bindingComponentID]("target")
	keeper := catalogComponent[secondBindingComponentID]("keeper")
	binding := codec.BindDecoder(format.NewTag("fixture", "removed"), codec.New(target.Identity()))
	definition := plugin.Define[bindingPluginID](plugin.Descriptor{DisplayName: "binding plugin", Version: "1"}, target, keeper).
		WithDeclarations(binding)

	_, err := Build(plugin.NewSet(definition).Remove(target.Identity()))
	if err == nil {
		t.Fatal("removed owned declaration target unexpectedly accepted")
	}
	for _, item := range diagnosticItems(err) {
		if item.Code == "catalog.declaration-target" &&
			item.Path.Component == target.Identity().String() &&
			item.Detail["ownerDefinition"] == definition.Identity().String() {
			return
		}
	}
	t.Fatalf("target/owner diagnostic missing: %v", err)
}

func TestMetadataBindingTargetRequiresEncodingTrait(t *testing.T) {
	slot := carrier.Define[metadataBindingCarrierID]()
	valid := metadataBindingComponent()
	definition := plugin.Define[bindingPluginID](plugin.Descriptor{DisplayName: "binding plugin", Version: "1"}, valid)
	index, err := Build(plugin.NewSet(definition).AddDeclaration(metadata.Bind(slot, valid.Identity())))
	if err != nil {
		t.Fatalf("valid metadata encoding rejected: %v", err)
	}
	declaration, ok := index.LookupDeclaration(metadata.BindingKey(slot))
	if !ok || !declaration.SameTargets(metadata.Bind(slot, valid.Identity())) {
		t.Fatalf("metadata binding lookup = %#v/%v", declaration, ok)
	}

	invalid := catalogComponent[bindingComponentID]("not an encoding")
	definition = plugin.Define[bindingPluginID](plugin.Descriptor{DisplayName: "binding plugin", Version: "1"}, invalid)
	_, err = Build(plugin.NewSet(definition).AddDeclaration(metadata.Bind(slot, invalid.Identity())))
	if err == nil || !hasCatalogDiagnostic(err, "catalog.metadata-encoding") {
		t.Fatalf("metadata trait diagnostic = %v", err)
	}
}

func TestBuildRejectsInvalidMetadataEncodingTrait(t *testing.T) {
	component := plugin.NewComponent[metadataBindingComponentID](
		plugin.Descriptor{DisplayName: "invalid encoding"},
		config.Struct[metadataBindingConfigID](func() struct{} { return struct{}{} }).Version("1").Build(),
		metadata.WithEncoding(nil, nil),
	)
	definition := plugin.Define[bindingPluginID](plugin.Descriptor{DisplayName: "binding plugin", Version: "1"}, component)
	if _, err := Build(plugin.NewSet(definition)); err == nil || !hasCatalogDiagnostic(err, "catalog.metadata-trait") {
		t.Fatalf("invalid metadata trait diagnostic = %v", err)
	}
}

func metadataBindingComponent() plugin.Component {
	schema := config.Struct[metadataBindingConfigID](func() struct{} { return struct{}{} }).Version("1").Build()
	return plugin.NewComponent[metadataBindingComponentID](plugin.Descriptor{DisplayName: "encoding"}, schema, metadata.WithEncoding(
		func(ctx metadata.ParseContext) (metadata.Document, error) {
			return metadata.NewBuilder(ctx.Scope()).Build()
		},
		func(metadata.MarshalContext) (metadata.Blob, []loss.Loss, error) {
			return metadata.NewBlob("", nil), nil, nil
		},
	))
}

func TestBindingRegistrationOrderDoesNotChangeFingerprint(t *testing.T) {
	definition := plugin.Define[bindingPluginID](plugin.Descriptor{DisplayName: "binding plugin", Version: "1"}, catalogComponent[bindingComponentID]("binding"))
	target := codec.New(plugin.IdentityOf[bindingComponentID]())
	first := codec.BindDecoder(format.NewTag("fixture", "a"), target)
	second := codec.BindDecoder(format.NewTag("fixture", "b"), target)
	left, err := Build(plugin.NewSet(definition).AddDeclaration(first).AddDeclaration(second))
	if err != nil {
		t.Fatal(err)
	}
	right, err := Build(plugin.NewSet(definition).AddDeclaration(second).AddDeclaration(first))
	if err != nil {
		t.Fatal(err)
	}
	if left.Fingerprint() != right.Fingerprint() {
		t.Fatal("binding registration order changed catalog fingerprint")
	}
}

func TestCodecBindingsAreIndexedByTargetTagAndRole(t *testing.T) {
	codecComponent := catalogComponent[bindingComponentID]("codec")
	parserComponent := catalogComponent[secondBindingComponentID]("parser")
	definition := plugin.Define[bindingPluginID](plugin.Descriptor{DisplayName: "binding plugin", Version: "1"}, codecComponent, parserComponent)
	firstTag := format.NewTag("fixture", "a")
	secondTag := format.NewTag("fixture", "b")
	set := plugin.NewSet(definition).
		AddDeclaration(codec.BindDecoder(firstTag, codec.New(codecComponent.Identity()))).
		AddDeclaration(codec.BindParser(firstTag, codec.NewParser(parserComponent.Identity()))).
		AddDeclaration(codec.BindDecoder(secondTag, codec.New(codecComponent.Identity())))
	index, err := Build(set)
	if err != nil {
		t.Fatal(err)
	}
	codecValues := index.CodecBindings(codecComponent.Identity())
	if len(codecValues) != 2 || codecValues[0].Tag() != firstTag || codecValues[0].Role() != codec.DecoderRole || codecValues[1].Tag() != secondTag || codecValues[1].Role() != codec.DecoderRole {
		t.Fatalf("codec reverse bindings = %#v", codecValues)
	}
	parserValues := index.CodecBindings(parserComponent.Identity())
	if len(parserValues) != 1 || parserValues[0].Tag() != firstTag || parserValues[0].Role() != codec.ParserRole {
		t.Fatalf("parser reverse bindings = %#v", parserValues)
	}
	parserValues[0] = CodecBinding{}
	if got := index.CodecBindings(parserComponent.Identity()); len(got) != 1 || got[0].Tag() != firstTag {
		t.Fatal("codec reverse binding lookup exposed index storage")
	}
}

func diagnosticItems(err error) []diagnostic.Item { return diagnostic.ItemsOf(err) }
