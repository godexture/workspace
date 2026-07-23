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

// registerMixer registers "mixer" as a parameterized filter: --in/--out
// (MixerParameters) fix the port topology before MixerConfig is even
// resolved, since InputRequirements below depends on how many input ports
// there are. The CLI-registered mixer always mixes every input with equal
// weight (1/Inputs); arbitrary per-input weighting is a Go-API-only
// concern reached via mixer.New directly, not through this registration.
func registerMixer() {
	if err := godec.Register(registry.ParameterizedFilterManifest{
		BaseManifest: registry.BaseManifest{
			Name:                 "mixer",
			Description:          "Mix N input ports into M output ports with equal weight (also serves as a tee when in=1)",
			ConfigurationFactory: registry.NewConfigurationFactory(NewMixerParameters),
		},
		NewManifest: func(parameters registry.Configuration) (registry.FilterManifest, error) {
			value, err := engine.ResolveConfig[config.MixerParameters, MixerParameters](parameters)
			if err != nil {
				return registry.FilterManifest{}, err
			}
			inputs, outputs := value.Inputs, value.Outputs

			requirements := make(registry.InputRequirements, inputs)
			for i := 0; i < inputs; i++ {
				requirements[fmt.Sprintf("in%d", i)] = registry.StaticRequirements(&manifest.AudioConstraint{})
			}

			return registry.FilterManifest{
				TransformManifest: registry.TransformManifest{
					BaseManifest: registry.BaseManifest{
						Name:                 "mixer",
						Description:          "Mix N input ports into M output ports with equal weight",
						ConfigurationFactory: registry.NewConfigurationFactory(NewMixerConfig),
					},
					InputRequirements: requirements,
				},
				Factory: func(in media.StreamSet, options registry.TransformFactoryOptions) (node.Filter, media.StreamSet, error) {
					if _, err := engine.ResolveConfig[config.MixerConfig, MixerConfig](options.Config); err != nil {
						return nil, nil, err
					}
					item, err := mixer.New(inputs, outputs, uniformWeights(inputs, outputs), false)
					if err != nil {
						return nil, nil, err
					}
					// Every input port shares the same stream shape (the mixer
					// engine rejects mismatched formats at runtime), so any one
					// of them describes every output port too.
					reference := in["in0"]
					result := make(media.StreamSet, outputs)
					for o := 0; o < outputs; o++ {
						result[fmt.Sprintf("out%d", o)] = reference
					}
					return item, result, nil
				},
			}, nil
		},
	}); err != nil {
		panic(err)
	}
}

// uniformWeights gives every output an identical average of every input
// (weight 1/inputs each): safe by construction (each row's L1 norm is
// exactly 1, so it can never clip) regardless of topology, which is why
// the CLI doesn't need to expose normalization as a separate setting.
func uniformWeights(inputs, outputs int) [][]float64 {
	weight := 1 / float64(inputs)
	rows := make([][]float64, outputs)
	for o := range rows {
		row := make([]float64, inputs)
		for i := range row {
			row[i] = weight
		}
		rows[o] = row
	}
	return rows
}
