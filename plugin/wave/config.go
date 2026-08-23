package wave

import (
	"github.com/godexture/godec/config"
)

type (
	configID    struct{}
	muxConfigID struct{}
)

type configuration struct{}

func configurationSchema() config.Schema[configuration] {
	return config.Struct[configID](func() configuration { return configuration{} }).Version("1").Build()
}

// muxConfiguration lets a caller ask for a codec other than the one its input
// already uses. The zero value keeps the input codec, which is what makes a
// conversion an explicit request rather than something a default does quietly.
type muxConfiguration struct {
	Codec string
}

func muxConfigurationSchema() config.Schema[muxConfiguration] {
	choices := []config.Choice[string]{{ID: "source", Label: "Keep the source codec", Value: ""}}
	for _, entry := range waveCodecs {
		choices = append(choices, config.Choice[string]{ID: entry.name, Label: entry.label, Value: entry.name})
	}
	return config.Struct[muxConfigID](func() muxConfiguration { return muxConfiguration{} }).
		Version("1").
		AddField(config.Field("codec", func(value *muxConfiguration) *string { return &value.Codec }, config.Enum(choices...))).
		Build()
}
