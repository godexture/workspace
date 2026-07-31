package filter

import (
	"math"

	godec "github.com/godexture/godec/core"
	"github.com/godexture/godec/core/domain/manifest"
	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/node"
	"github.com/godexture/godec/core/registry"
	"github.com/godexture/godec/sdk/engine"
)

func speedRelabelRate(rate int, factor float64) int {
	target := int(math.Round(float64(rate) * factor))
	if target < 1 {
		target = 1
	}
	return target
}

// registerSimple registers a filter whose factory does nothing beyond
// resolving its configuration, constructing the engine, and wrapping it —
// the shape shared by every effect below that neither overrides the stream
// transform nor needs a bridge.
func registerSimple[T any, C engine.Wrapper[T], E engine.FilterEngine](newConfig registry.ConfigurationFactory, name, description string, newEngine func(T) (E, error)) {
	register(newConfig, name, description, identityTransform, func(cfg registry.Configuration) (node.Filter, error) {
		value, err := engine.ResolveConfig[T, C](cfg)
		if err != nil {
			return nil, err
		}
		item, err := newEngine(value)
		if err != nil {
			return nil, err
		}
		return engine.WrapFilter(item), nil
	}, nil, nil)
}

func register(newConfig registry.ConfigurationFactory, name, description string, transform func(media.StreamInfo, registry.Configuration) (media.Profile, error), factory func(registry.Configuration) (node.Filter, error), bridge registry.BridgeFunc, transformStream func(media.StreamInfo, media.CodecID, registry.Configuration) (media.StreamInfo, error)) {
	if err := godec.Register(registry.FilterManifest{TransformManifest: registry.TransformManifest{BaseManifest: registry.BaseManifest{Name: name, Description: description, ConfigurationFactory: newConfig}, InputRequirements: registry.SingleInputRequirements(registry.StaticRequirements(&manifest.AudioConstraint{}))}, OutputPorts: []string{"out"}, Bridge: registry.SingleInputBridge(bridge), Factory: registry.SingleFactory(func(in media.StreamInfo, options registry.TransformFactoryOptions) (node.Filter, media.StreamInfo, error) {
		item, err := factory(options.Config)
		if err != nil {
			return nil, media.StreamInfo{}, err
		}
		if transformStream != nil {
			output, err := transformStream(in, in.Codec, options.Config)
			if err != nil {
				_ = item.Close()
				return nil, media.StreamInfo{}, err
			}
			return item, output, nil
		}
		profile, err := transform(in, options.Config)
		if err != nil {
			_ = item.Close()
			return nil, media.StreamInfo{}, err
		}
		in.Type = profile.Type
		in.MediaAttributes = profile.MediaAttributes
		return item, in, nil
	})}); err != nil {
		panic(err)
	}
}

func copyProfile(in media.StreamInfo) media.Profile {
	return media.Profile{Type: in.Type, MediaAttributes: in.MediaAttributes}
}
func identityTransform(in media.StreamInfo, _ registry.Configuration) (media.Profile, error) {
	return copyProfile(in), nil
}
