package wave

import (
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/media/sample"
)

type (
	configID    struct{}
	muxConfigID struct{}
)

type configuration struct{}

func configurationSchema() config.Schema[configuration] {
	return config.Struct[configID](func() configuration { return configuration{} }).Version("1").Build()
}

// muxConfiguration lets a caller ask for a coding other than the one its input
// already uses. The zero value keeps the source coding, which is what makes a
// conversion an explicit request rather than something a default does quietly.
type muxConfiguration struct {
	Coding sample.Coding
}

func muxConfigurationSchema() config.Schema[muxConfiguration] {
	choices := []config.Choice[sample.Coding]{{ID: "source", Label: "Keep the source coding", Value: ""}}
	for _, coding := range Codings() {
		choices = append(choices, config.Choice[sample.Coding]{ID: string(coding), Label: codingLabels[coding], Value: coding})
	}
	return config.Struct[muxConfigID](func() muxConfiguration { return muxConfiguration{} }).
		Version("1").
		AddField(config.Field("coding", func(value *muxConfiguration) *sample.Coding { return &value.Coding }, config.Enum(choices...))).
		Build()
}

var codingLabels = map[sample.Coding]string{
	sample.U8:  "Unsigned 8-bit",
	sample.S16: "Signed 16-bit",
	sample.S24: "Signed 24-bit",
	sample.S32: "Signed 32-bit",
	sample.F32: "32-bit float",
	sample.F64: "64-bit float",
}
