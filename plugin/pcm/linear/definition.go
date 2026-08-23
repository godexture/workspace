// Package linear implements raw signed linear PCM on the typed component
// stack. It covers every scalar coding the sample vocabulary names; a
// container decides which of them a stream uses.
package linear

import (
	"github.com/godexture/godec/access"
	"github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/plugin"
)

type (
	pluginID struct{}
	readerID struct{}
	parserID struct{}
	writerID struct{}
	rawID    struct{}

	decoderS16ID struct{}
	decoderS32ID struct{}
	decoderF32ID struct{}
	decoderF64ID struct{}
	encoderS16ID struct{}
	encoderS32ID struct{}
	encoderF32ID struct{}
	encoderF64ID struct{}
)

func ReaderIdentity() plugin.Identity { return plugin.IdentityOf[readerID]() }
func ParserIdentity() plugin.Identity { return plugin.IdentityOf[parserID]() }
func WriterIdentity() plugin.Identity { return plugin.IdentityOf[writerID]() }

var (
	decoderIdentities = map[sample.Coding]plugin.Identity{
		sample.S16: plugin.IdentityOf[decoderS16ID](),
		sample.S32: plugin.IdentityOf[decoderS32ID](),
		sample.F32: plugin.IdentityOf[decoderF32ID](),
		sample.F64: plugin.IdentityOf[decoderF64ID](),
	}
	encoderIdentities = map[sample.Coding]plugin.Identity{
		sample.S16: plugin.IdentityOf[encoderS16ID](),
		sample.S32: plugin.IdentityOf[encoderS32ID](),
		sample.F32: plugin.IdentityOf[encoderF32ID](),
		sample.F64: plugin.IdentityOf[encoderF64ID](),
	}
)

// DecoderIdentity returns the decoder that reads coding from the wire. Frame
// ports are static, so codings sharing a canonical representation share one
// decoder and the rest have their own.
func DecoderIdentity(coding sample.Coding) plugin.Identity {
	return decoderIdentities[coding.Decoded()]
}

// EncoderIdentity returns the encoder that writes coding to the wire.
func EncoderIdentity(coding sample.Coding) plugin.Identity {
	return encoderIdentities[coding.Decoded()]
}

// Raw is the direction-neutral identity of containerless linear PCM.
func Raw() format.Format {
	value, err := format.Define[rawID](nil, format.WithExtensions("raw", "pcm"))
	if err != nil {
		panic(err)
	}
	return value
}

func operationFormat(kind operation) plugin.ComponentOption {
	switch kind {
	case readerOperation:
		return format.Read(Raw(), access.NewRequirements(
			access.AllOf(access.SequentialRead),
			access.AllOf(access.RandomRead, access.StableSize),
		), format.WithProbe(probeRaw), format.RequireFallbackConfig("rate", "coding", "layout", "endian"))
	case writerOperation:
		return format.Write(Raw(), access.NewRequirements(access.AllOf(access.SequentialWrite)))
	default:
		return nil
	}
}

// Plugin returns the pure-Go component family without global registration.
// A headerless stream carries no codec tag, so this family declares no codec
// binding: the container that names a coding is what a composition binds.
func Plugin() plugin.Definition {
	descriptor := plugin.Descriptor{
		DisplayName: "Linear PCM",
		Version:     "0.1.0",
		License:     "MIT",
		Build:       plugin.BuildModePureGo,
	}
	definition := plugin.Define[pluginID](descriptor,
		newFraming[readerID](readerOperation, "Raw PCM reader"),
		newFraming[parserID](parserOperation, "Linear PCM parser"),
		newFraming[writerID](writerOperation, "Raw PCM writer"),
		newCodec[decoderS16ID, int16](decoderOperation, "Linear PCM decoder to s16 frames"),
		newCodec[decoderS32ID, int32](decoderOperation, "Linear PCM decoder to s32 frames"),
		newCodec[decoderF32ID, float32](decoderOperation, "Linear PCM decoder to f32 frames"),
		newCodec[decoderF64ID, float64](decoderOperation, "Linear PCM decoder to f64 frames"),
		newCodec[encoderS16ID, int16](encoderOperation, "Linear PCM encoder from s16 frames"),
		newCodec[encoderS32ID, int32](encoderOperation, "Linear PCM encoder from s32 frames"),
		newCodec[encoderF32ID, float32](encoderOperation, "Linear PCM encoder from f32 frames"),
		newCodec[encoderF64ID, float64](encoderOperation, "Linear PCM encoder from f64 frames"),
	)
	return definition.WithDeclarations(sample.Declarations()...)
}

// Set returns the self-contained composition, including the sample property
// vocabulary.
func Set() plugin.Set {
	return plugin.NewSet(Plugin())
}
