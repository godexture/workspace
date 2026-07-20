package resolver

import (
	"errors"
	"strings"
	"testing"

	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
)

type invalidDecoderConfig struct{}
type acceptingDecoderConfig struct{}

func TestDefaultDecoderResolverReportsRequirementErrorsWhenNoDecoderMatches(t *testing.T) {
	t.Parallel()
	decoders := registry.NewRegistry[registry.DecoderManifest]()
	if err := decoders.Register(invalidDecoderConfig{}, decoderManifest("invalid", func(media.CodecID, registry.Configuration) ([]manifest.Capability, error) {
		return nil, errors.New("invalid decoder requirements")
	})); err != nil {
		t.Fatal(err)
	}

	_, err := NewDefaultDecoderResolver(decoders).ResolveDecoder(media.StreamInfo{Type: media.MediaAudio})
	if err == nil || !strings.Contains(err.Error(), "invalid decoder requirements") {
		t.Fatalf("ResolveDecoder() error = %v, want requirement error", err)
	}
}

func TestDefaultDecoderResolverAcceptsValidCandidateAfterRequirementError(t *testing.T) {
	t.Parallel()
	decoders := registry.NewRegistry[registry.DecoderManifest]()
	if err := decoders.Register(invalidDecoderConfig{}, decoderManifest("invalid", func(media.CodecID, registry.Configuration) ([]manifest.Capability, error) {
		return nil, errors.New("invalid decoder requirements")
	})); err != nil {
		t.Fatal(err)
	}
	if err := decoders.Register(acceptingDecoderConfig{}, decoderManifest("accepting", registry.StaticRequirements(&manifest.AudioConstraint{}))); err != nil {
		t.Fatal(err)
	}

	resolved, err := NewDefaultDecoderResolver(decoders).ResolveDecoder(media.StreamInfo{Type: media.MediaAudio})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := resolved.Name, "accepting"; got != want {
		t.Fatalf("resolved decoder = %q, want %q", got, want)
	}
}

func decoderManifest(name string, requirements registry.InputRequirementsFunc) registry.DecoderManifest {
	return registry.DecoderManifest{
		TransformManifest: registry.TransformManifest{
			BaseManifest:      registry.BaseManifest{Name: name},
			InputRequirements: requirements,
		},
		Factory: func(media.StreamInfo, registry.TransformFactoryOptions) (node.Decoder, error) { return nil, nil },
	}
}
