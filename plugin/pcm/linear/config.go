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
		Layout:       sample.Mono(),
		Endian:       sample.LittleEndian,
		ChunkSamples: 1024,
	}
}

func configurationSchema() config.Schema[configuration] {
	return config.Struct[configID](defaultConfiguration).
		Version("1").
		AddField(config.Field("rate", func(value *configuration) *int { return &value.Rate }, config.Int().Range(1, 768_000), config.Unit("Hz"))).
		AddField(config.Field("validBits", func(value *configuration) *int { return &value.ValidBits }, config.Int().Range(1, 16))).
		AddField(config.Field("layout", func(value *configuration) *sample.Layout { return &value.Layout }, sample.LayoutCodec())).
		AddField(config.Field("endian", func(value *configuration) *sample.Endian { return &value.Endian }, sample.EndianCodec())).
		AddField(config.Field("chunkSamples", func(value *configuration) *int { return &value.ChunkSamples }, config.Int().Range(1, 1<<20))).
		Build()
}

func (c configuration) wire() sample.Description {
	return sample.Description{
		Coding:    sample.S16,
		Packing:   sample.Interleaved,
		Endian:    c.Endian,
		Rate:      c.Rate,
		Layout:    c.Layout,
		ValidBits: c.ValidBits,
	}
}

func (c configuration) planar() sample.Description { return c.wire().Decoded() }

func suggestConfiguration(input sample.Description, desired *sample.Description) (configuration, bool) {
	result := defaultConfiguration()
	if input.Coding != sample.S16 {
		return configuration{}, false
	}
	if input.Packing == sample.Interleaved {
		result.Endian = input.Endian
	}
	result.Rate = input.Rate
	result.ValidBits = input.ValidBits
	result.Layout = input.Layout
	if desired != nil && desired.Packing == sample.Interleaved {
		result.Endian = desired.Endian
	}
	return result, result.wire().Valid() && result.planar().Valid()
}
