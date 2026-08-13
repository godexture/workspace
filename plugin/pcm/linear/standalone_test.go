package linear_test

import (
	"testing"

	"github.com/godexture/godec/host"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/plugin/pcm/linear"
)

// A plugin that contributes a codec binding must compose on its own, without
// the caller knowing which vocabulary the binding implies.
func TestPluginComposesAloneWithItsBindingVocabulary(t *testing.T) {
	instance, err := host.New(host.Plugins(plugin.NewSet(linear.Plugin())))
	if err != nil {
		t.Fatalf("standalone composition failed: %v", err)
	}
	declared := 0
	for _, declaration := range instance.Catalog().Declarations() {
		if _, ok := declaration.Targets()[0].Type(); ok {
			declared++
		}
	}
	if declared == 0 {
		t.Fatal("binding did not carry its key vocabulary into the catalog")
	}
}
