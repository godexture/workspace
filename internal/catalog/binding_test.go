package catalog

import (
	"testing"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/media/codec"
	"github.com/godexture/godec/media/format"
	"github.com/godexture/godec/plugin"
)

type bindingPluginID struct{}
type bindingComponentID struct{}
type secondBindingComponentID struct{}

func TestBuildRejectsConflictingCodecBindings(t *testing.T) {
	definition := plugin.Define[bindingPluginID](plugin.Descriptor{DisplayName: "binding plugin", Version: "1"}, catalogComponent[bindingComponentID]("binding"))
	key := format.NewTag("fixture", "tag")
	first := codec.BindWithoutParser(key, codec.New(plugin.IdentityOf[bindingComponentID]()))
	second := codec.BindWithoutParser(key, codec.New(plugin.IdentityOf[secondBindingComponentID]()))
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
	missing := codec.BindWithoutParser(format.NewTag("fixture", "missing"), codec.New(plugin.IdentityOf[secondBindingComponentID]()))
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

func TestBindingRegistrationOrderDoesNotChangeFingerprint(t *testing.T) {
	definition := plugin.Define[bindingPluginID](plugin.Descriptor{DisplayName: "binding plugin", Version: "1"}, catalogComponent[bindingComponentID]("binding"))
	target := codec.New(plugin.IdentityOf[bindingComponentID]())
	first := codec.BindWithoutParser(format.NewTag("fixture", "a"), target)
	second := codec.BindWithoutParser(format.NewTag("fixture", "b"), target)
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

func diagnosticItems(err error) []diagnostic.Item { return diagnostic.ItemsOf(err) }
