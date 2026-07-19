package registry

import (
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/node"
)

type alphaConfig struct{}
type betaConfig struct{}

func TestPluginKeyIsDerivedFromRoleAndNamedConfigurationType(t *testing.T) {
	t.Parallel()
	muxers := NewRegistry[MuxerManifest]()
	demuxers := NewRegistry[DemuxerManifest]()

	valueKey, err := muxers.Key(alphaConfig{})
	if err != nil {
		t.Fatal(err)
	}
	pointerKey, err := muxers.Key(&alphaConfig{})
	if err != nil {
		t.Fatal(err)
	}
	demuxerKey, err := demuxers.Key(alphaConfig{})
	if err != nil {
		t.Fatal(err)
	}

	if valueKey != pointerKey {
		t.Fatalf("pointer and value configurations produced different keys: %s != %s", valueKey, pointerKey)
	}
	if valueKey == demuxerKey {
		t.Fatalf("different roles produced the same key: %s", valueKey)
	}
	if valueKey.Role() != manifest.RoleMuxer {
		t.Fatalf("role = %s, want %s", valueKey.Role(), manifest.RoleMuxer)
	}
	if valueKey.ConfigurationType() != reflect.TypeOf(alphaConfig{}) {
		t.Fatalf("configuration type = %s", valueKey.ConfigurationType())
	}
	if !strings.Contains(valueKey.String(), "registry.alphaConfig") {
		t.Fatalf("diagnostic key does not identify the configuration type: %s", valueKey)
	}
}

func TestRegistryRejectsInvalidConfigurationIdentity(t *testing.T) {
	t.Parallel()
	registry := NewRegistry[MuxerManifest]()
	for name, config := range map[string]Configuration{
		"nil":       nil,
		"typed nil": (*alphaConfig)(nil),
		"builtin":   1,
		"unnamed":   struct{}{},
		"map":       map[string]int{},
	} {
		t.Run(name, func(t *testing.T) {
			if err := registry.Register(config, testMuxerManifest("muxer")); err == nil {
				t.Fatalf("Register(%T) succeeded", config)
			}
		})
	}
}

func TestRegistryRejectsDuplicateKeyWithoutReplacingOriginal(t *testing.T) {
	t.Parallel()
	registry := NewRegistry[MuxerManifest]()
	if err := registry.Register(alphaConfig{}, testMuxerManifest("original")); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(&alphaConfig{}, testMuxerManifest("replacement")); err == nil {
		t.Fatal("duplicate registration succeeded")
	}
	key, _ := registry.Key(alphaConfig{})
	got, err := registry.Get(key)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "original" {
		t.Fatalf("duplicate registration replaced original with %q", got.Name)
	}
}

func TestRegistryEnumerationIsDeterministic(t *testing.T) {
	t.Parallel()
	registry := NewRegistry[MuxerManifest]()
	if err := registry.Register(betaConfig{}, testMuxerManifest("beta")); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(alphaConfig{}, testMuxerManifest("alpha")); err != nil {
		t.Fatal(err)
	}

	first := slices.Collect(registry.Enumerate())
	second := slices.Collect(registry.Enumerate())
	firstKeys := []PluginKey{first[0].ID(), first[1].ID()}
	secondKeys := []PluginKey{second[0].ID(), second[1].ID()}
	if !reflect.DeepEqual(firstKeys, secondKeys) {
		t.Fatalf("enumeration changed order: %v != %v", firstKeys, secondKeys)
	}
	if first[0].Name != "alpha" || first[1].Name != "beta" {
		t.Fatalf("enumeration is not key-sorted: %#v", first)
	}
}

func testMuxerManifest(name string) MuxerManifest {
	return MuxerManifest{
		BaseManifest: BaseManifest{Name: name},
		Factory: func(io.Writer, Configuration) (node.Muxer, error) {
			return nil, nil
		},
	}
}
