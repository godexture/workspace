package linear

import (
	"testing"

	"github.com/godexture/godec/host"
	"github.com/godexture/godec/plugin"
)

func TestSetIsExactlyTheOwnedPluginComposition(t *testing.T) {
	actual, err := host.New(host.Plugins(Set()))
	if err != nil {
		t.Fatal(err)
	}
	expected, err := host.New(host.Plugins(plugin.NewSet(Plugin())))
	if err != nil {
		t.Fatal(err)
	}
	if actual.Catalog().Fingerprint() != expected.Catalog().Fingerprint() {
		t.Fatal("linear.Plugin does not own its complete composition")
	}
}
