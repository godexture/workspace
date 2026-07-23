package filter

import (
	"fmt"

	godec "github.com/godexture/core"
	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
	"github.com/godexture/filter-audio/internal/config"
	"github.com/godexture/filter-audio/internal/mixer"
	"github.com/godexture/sdk/engine"
)

func init() {
	registerMixer()
}

func registerMixer() {
	requirements := make(registry.InputRequirements, config.MaxMixerPorts)
	for i := 0; i < config.MaxMixerPorts; i++ {
		requirements[fmt.Sprintf("in%d", i)] = registry.StaticRequirements(&manifest.AudioConstraint{})
	}

	if err := godec.Register(registry.FilterManifest{TransformManifest: registry.TransformManifest{
		BaseManifest:      registry.BaseManifest{Name: "mixer", Description: "Mix N input ports into M output ports through a linear weight matrix (also serves as a tee when N=1)", ConfigurationFactory: registry.NewConfigurationFactory(NewMixerConfig)},
		InputRequirements: requirements,
		Resources:         registry.ResourceRequest{},
	}, Factory: func(in media.StreamInfo, options registry.TransformFactoryOptions) (node.Filter, media.StreamInfo, error) {
		value, err := engine.ResolveConfig[config.MixerConfig, MixerConfig](options.Config)
		if err != nil {
			return nil, media.StreamInfo{}, err
		}
		item, err := mixer.New(value)
		if err != nil {
			return nil, media.StreamInfo{}, err
		}

		inputs := make([]engine.FilterInput, len(item.InputIDs()))
		for i, id := range item.InputIDs() {
			inputs[i] = engine.FilterInput{ID: id, Phase: node.InputPhaseRun}
		}
		return engine.WrapFilter(item,
			engine.WithInputs(inputs...),
			engine.WithOutputs(item.OutputIDs()...),
		), in, nil
	}}); err != nil {
		panic(err)
	}
}
