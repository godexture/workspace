package linear

import (
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/media/sample"
)

type configID struct{}

type configuration struct {
	Coding       sample.Coding
	Endian       sample.Endian
	Rate         int
	Layout       sample.Layout
	ValidBits    int
	ChunkSamples int
}

func defaultConfiguration() configuration {
	return configuration{
		Coding:       sample.S16,
		Endian:       sample.LittleEndian,
		Rate:         48_000,
		Layout:       sample.Mono(),
		ChunkSamples: 1024,
	}
}

func configurationSchema() config.Schema[configuration] {
	return config.Struct[configID](defaultConfiguration).
		Version("1").
		AddField(config.Field("coding", func(value *configuration) *sample.Coding { return &value.Coding }, sample.CodingCodec())).
		AddField(config.Field("endian", func(value *configuration) *sample.Endian { return &value.Endian }, sample.EndianCodec())).
		AddField(config.Field("rate", func(value *configuration) *int { return &value.Rate }, config.Int().Range(1, 768_000), config.Unit("Hz"))).
		AddField(config.Field("layout", func(value *configuration) *sample.Layout { return &value.Layout }, sample.LayoutCodec())).
		AddField(config.Field("validBits", func(value *configuration) *int { return &value.ValidBits }, config.Int().Range(0, 64).Help("bits carrying information, or 0 for the coding's full width"))).
		AddField(config.Field("chunkSamples", func(value *configuration) *int { return &value.ChunkSamples }, config.Int().Range(1, 1<<20))).
		Build()
}

// wire is the interleaved description this configuration reads or writes.
// Byte order is a property of multi-byte codings only, so a single-byte coding
// normalizes it away instead of rejecting the operator's choice.
func (c configuration) wire() sample.Description {
	endian := c.Endian
	if c.Coding.Bytes() == 1 {
		endian = sample.NoEndian
	}
	bits := c.ValidBits
	if bits <= 0 {
		bits = c.Coding.Bits()
	}
	return sample.Description{
		Coding:    c.Coding,
		Packing:   sample.Interleaved,
		Endian:    endian,
		Rate:      c.Rate,
		Layout:    c.Layout,
		ValidBits: bits,
	}
}

func (c configuration) planar() sample.Description { return c.wire().Decoded() }

// suggestConfiguration derives a configuration from the stream this component
// was handed and, when a downstream node asked for a particular wire shape,
// from that request. It never invents a coding neither side named.
func suggestConfiguration(input sample.Description, desired *sample.Description) (configuration, bool) {
	result := defaultConfiguration()
	result.Rate = input.Rate
	result.Layout = input.Layout
	result.Coding = input.Coding
	result.ValidBits = input.ValidBits
	if input.Packing == sample.Interleaved && input.Endian != sample.NoEndian {
		result.Endian = input.Endian
	}
	if desired != nil && desired.Packing == sample.Interleaved {
		result.Coding = desired.Coding
		result.ValidBits = desired.ValidBits
		if desired.Endian != sample.NoEndian {
			result.Endian = desired.Endian
		}
	}
	return result, result.wire().Valid() && result.planar().Valid()
}
