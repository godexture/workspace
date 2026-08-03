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

func TestBuildRejectsConflictingCodecBindings(t *testing.T) {
	definition := plugin.Define[bindingPluginID](plugin.Descriptor{DisplayName: "binding plugin", Version: "1"}, catalogComponent[bindingComponentID]("binding"))
	key := format.NewTag("fixture", "tag")
	first := codec.BindWithoutParser(key, codec.New("fixture:first"))
	second := codec.BindWithoutParser(key, codec.New("fixture:second"))
	_, err := Build(plugin.NewSet(definition).AddBinding(first).AddBinding(second))
	if err == nil {
		t.Fatal("conflicting binding unexpectedly accepted")
	}
	for _, item := range diagnosticItems(err) {
		if item.Code == "catalog.binding-conflict" {
			return
		}
	}
	t.Fatalf("binding conflict diagnostic missing: %v", err)
}

func TestBindingRegistrationOrderDoesNotChangeFingerprint(t *testing.T) {
	definition := plugin.Define[bindingPluginID](plugin.Descriptor{DisplayName: "binding plugin", Version: "1"}, catalogComponent[bindingComponentID]("binding"))
	first := codec.BindWithoutParser(format.NewTag("fixture", "a"), codec.New("fixture:a"))
	second := codec.BindWithoutParser(format.NewTag("fixture", "b"), codec.New("fixture:b"))
	left, err := Build(plugin.NewSet(definition).AddBinding(first).AddBinding(second))
	if err != nil {
		t.Fatal(err)
	}
	right, err := Build(plugin.NewSet(definition).AddBinding(second).AddBinding(first))
	if err != nil {
		t.Fatal(err)
	}
	if left.Fingerprint() != right.Fingerprint() {
		t.Fatal("binding registration order changed catalog fingerprint")
	}
}

func diagnosticItems(err error) []diagnostic.Item { return diagnostic.ItemsOf(err) }
