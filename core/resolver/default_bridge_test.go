package resolver

import (
	"context"
	"fmt"
	"testing"

	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
)

type bridgeFormatConfig struct{ format media.SampleFormat }
type bridgeRateConfig struct{ rate int }
type bridgeCombinedConfig struct {
	format media.SampleFormat
	rate   int
}
type bridgeNoopConfig struct{}

type bridgeFilterNode struct{}

func (*bridgeFilterNode) Start(context.Context) error                        { return nil }
func (*bridgeFilterNode) Close() error                                       { return nil }
func (*bridgeFilterNode) Process(context.Context) error                      { return nil }
func (*bridgeFilterNode) InputPorts() map[string]*node.InPort[media.Frame]   { return nil }
func (*bridgeFilterNode) OutputPorts() map[string]*node.OutPort[media.Frame] { return nil }

type bridgeAnyAudioCapability struct{}

func (bridgeAnyAudioCapability) Match(stream media.StreamInfo) bool {
	return stream.Type == media.MediaAudio
}
func (bridgeAnyAudioCapability) Diagnose(stream media.StreamInfo) error {
	if stream.Type == media.MediaAudio {
		return nil
	}
	return fmt.Errorf("not audio")
}

func TestDefaultBridgeResolverPrefersLowerQualityLoss(t *testing.T) {
	t.Parallel()
	filters := registry.NewRegistry[registry.FilterManifest]()
	target := &manifest.AudioConstraint{
		Codecs:      []media.CodecID{media.CodecLPCM},
		SampleRates: manifest.IntConstraint{Values: []int{48000}},
		SampleFormats: []manifest.SampleFormatConstraint{{
			Format: media.SampleFormatF32,
		}},
	}
	registerBridgeFilter(t, filters, bridgeFormatConfig{}, "format", func(current media.StreamInfo, required []manifest.Capability) ([]registry.ConversionCandidate, error) {
		if current.Audio.Format == media.SampleFormatF32 {
			return nil, nil
		}
		return []registry.ConversionCandidate{{Config: bridgeFormatConfig{format: media.SampleFormatF32}, Cost: registry.ConversionCost{Work: 1}}}, nil
	}, func(stream media.StreamInfo, config registry.Configuration) media.StreamInfo {
		stream.Audio.Format = config.(bridgeFormatConfig).format
		stream.Audio.BitsPerSample = 32
		return stream
	})
	registerBridgeFilter(t, filters, bridgeRateConfig{}, "rate", func(current media.StreamInfo, required []manifest.Capability) ([]registry.ConversionCandidate, error) {
		if current.Audio.SampleRate == 48000 {
			return nil, nil
		}
		return []registry.ConversionCandidate{{Config: bridgeRateConfig{rate: 48000}, Cost: registry.ConversionCost{Work: 1}}}, nil
	}, func(stream media.StreamInfo, config registry.Configuration) media.StreamInfo {
		stream.Audio.SampleRate = config.(bridgeRateConfig).rate
		return stream
	})
	registerBridgeFilter(t, filters, bridgeCombinedConfig{}, "combined", func(current media.StreamInfo, required []manifest.Capability) ([]registry.ConversionCandidate, error) {
		return []registry.ConversionCandidate{{
			Config: bridgeCombinedConfig{format: media.SampleFormatF32, rate: 48000},
			Cost:   registry.ConversionCost{QualityLoss: 1},
		}}, nil
	}, func(stream media.StreamInfo, config registry.Configuration) media.StreamInfo {
		resolved := config.(bridgeCombinedConfig)
		stream.Audio.Format = resolved.format
		stream.Audio.BitsPerSample = 32
		stream.Audio.SampleRate = resolved.rate
		return stream
	})

	source := media.StreamInfo{
		Type: media.MediaAudio,
		MediaAttributes: media.MediaAttributes{
			Codec: media.CodecLPCM,
			Audio: media.AudioAttributes{
				SampleRate:    44100,
				Format:        media.SampleFormatS16,
				BitsPerSample: 16,
				ChannelLayout: media.LayoutStereo2_0,
			},
		},
	}
	steps, err := NewDefaultBridgeResolver(filters).ResolveBridge(source, []manifest.Capability{target})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(steps))
	}
	if steps[0].Manifest.Name != "format" || steps[1].Manifest.Name != "rate" {
		t.Fatalf("bridge path = %s -> %s, want format -> rate", steps[0].Manifest.Name, steps[1].Manifest.Name)
	}
	if !target.Match(steps[len(steps)-1].Output) {
		t.Fatalf("bridge output does not match target: %v", target.Diagnose(steps[len(steps)-1].Output))
	}
}

func TestDefaultBridgeResolverRejectsNoopCandidate(t *testing.T) {
	t.Parallel()
	filters := registry.NewRegistry[registry.FilterManifest]()
	registerBridgeFilter(t, filters, bridgeNoopConfig{}, "noop", func(media.StreamInfo, []manifest.Capability) ([]registry.ConversionCandidate, error) {
		return []registry.ConversionCandidate{{Config: bridgeNoopConfig{}}}, nil
	}, func(stream media.StreamInfo, _ registry.Configuration) media.StreamInfo {
		return stream
	})
	_, err := NewDefaultBridgeResolver(filters).ResolveBridge(
		media.StreamInfo{Type: media.MediaAudio},
		[]manifest.Capability{&manifest.AudioConstraint{SampleRates: manifest.IntConstraint{Values: []int{48000}}}},
	)
	if err == nil {
		t.Fatal("ResolveBridge() succeeded for no-op candidate")
	}
}

func registerBridgeFilter(
	t *testing.T,
	filters *registry.FilterRegistry,
	config registry.Configuration,
	name string,
	bridge registry.BridgeFunc,
	transform func(media.StreamInfo, registry.Configuration) media.StreamInfo,
) {
	t.Helper()
	err := filters.Register(registry.FilterManifest{
		TransformManifest: registry.TransformManifest{
			BaseManifest:      registry.BaseManifest{Name: name, ConfigurationFactory: func() registry.Configuration { return config }},
			InputRequirements: registry.StaticRequirements(bridgeAnyAudioCapability{}),
		},
		Bridge: bridge,
		Factory: func(stream media.StreamInfo, options registry.TransformFactoryOptions) (node.Filter, media.StreamInfo, error) {
			return &bridgeFilterNode{}, transform(stream, options.Config), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}
