package linear

import (
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/media/sample"
)

type configID struct{}

type configuration struct {
	Rate         int
	ValidBits    int
	Layout       sample.Layout
	Endian       sample.Endian
	ChunkSamples int
}

func defaultConfiguration() configuration {
	return configuration{
		Rate:         48_000,
		ValidBits:    16,
		Layout:       sample.Mono,
		Endian:       sample.LittleEndian,
		ChunkSamples: 1024,
	}
}

func configurationSchema() config.Schema[configuration] {
	return config.Struct[configID](defaultConfiguration).
		Version("1").
		AddField(config.Field("rate", func(value *configuration) *int { return &value.Rate }, config.Int().Range(1, 768_000), config.Unit("Hz"))).
		AddField(config.Field("validBits", func(value *configuration) *int { return &value.ValidBits }, config.Int().Range(1, 16))).
		AddField(config.Field("layout", func(value *configuration) *sample.Layout { return &value.Layout }, config.Enum(
			config.Choice[sample.Layout]{ID: string(sample.Mono), Label: "Mono", Value: sample.Mono},
			config.Choice[sample.Layout]{ID: string(sample.Stereo), Label: "Stereo", Value: sample.Stereo},
		))).
		AddField(config.Field("endian", func(value *configuration) *sample.Endian { return &value.Endian }, config.Enum(
			config.Choice[sample.Endian]{ID: string(sample.LittleEndian), Label: "Little endian", Value: sample.LittleEndian},
			config.Choice[sample.Endian]{ID: string(sample.BigEndian), Label: "Big endian", Value: sample.BigEndian},
		))).
		AddField(config.Field("chunkSamples", func(value *configuration) *int { return &value.ChunkSamples }, config.Int().Range(1, 1<<20))).
		Build()
}

func (c configuration) wire() sample.Description {
	return sample.Description{
		Format:    sample.S16Interleaved,
		ValidBits: c.ValidBits,
		Rate:      c.Rate,
		Layout:    c.Layout,
		Endian:    c.Endian,
	}
}

func (c configuration) planar() sample.Description {
	return sample.Description{
		Format:    sample.S16Planar,
		ValidBits: c.ValidBits,
		Rate:      c.Rate,
		Layout:    c.Layout,
		Endian:    sample.NoEndian,
	}
}

func suggestConfiguration(input sample.Description, desired *sample.Description) (configuration, bool) {
	result := defaultConfiguration()
	switch input.Format {
	case sample.S16Interleaved:
		result.Endian = input.Endian
	case sample.S16Planar:
	default:
		return configuration{}, false
	}
	result.Rate = input.Rate
	result.ValidBits = input.ValidBits
	result.Layout = input.Layout
	if desired != nil && desired.Format == sample.S16Interleaved {
		result.Endian = desired.Endian
	}
	return result, result.wire().Valid() && result.planar().Valid()
}
