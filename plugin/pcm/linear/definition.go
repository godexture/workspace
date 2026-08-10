// Package linear implements raw signed linear PCM on the new typed component
// stack. It is deliberately separate from the legacy plugin/pcm internals.
package linear

import (
	"github.com/godexture/godec/access"
	"github.com/godexture/godec/media/codec"
	"github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/plugin"
)

type (
	pluginID  struct{}
	readerID  struct{}
	parserID  struct{}
	decoderID struct{}
	encoderID struct{}
	writerID  struct{}
	rawID     struct{}
)

func ReaderIdentity() plugin.Identity  { return plugin.IdentityOf[readerID]() }
func ParserIdentity() plugin.Identity  { return plugin.IdentityOf[parserID]() }
func DecoderIdentity() plugin.Identity { return plugin.IdentityOf[decoderID]() }
func EncoderIdentity() plugin.Identity { return plugin.IdentityOf[encoderID]() }
func WriterIdentity() plugin.Identity  { return plugin.IdentityOf[writerID]() }

// Raw is the direction-neutral identity of containerless signed PCM.
func Raw() format.Format {
	value, err := format.Define[rawID](nil)
	if err != nil {
		panic(err)
	}
	return value
}

func operationFormat(kind operation) plugin.ComponentOption {
	switch kind {
	case readerOperation:
		return format.Read(Raw(), access.NewRequirements(
			access.AnyOf(access.SequentialRead),
			access.AnyOf(access.RandomRead, access.StableSize),
		))
	case writerOperation:
		return format.Write(Raw(), access.AnyOf(access.SequentialWrite))
	default:
		return nil
	}
}

// Binding associates the raw PCM format tag with the first-class Parser and
// Decoder identities.
func Binding() plugin.Declaration {
	return codec.Bind(
		format.NewTag("pcm", "linear-s16"),
		codec.New(DecoderIdentity()),
		codec.NewParser(ParserIdentity()),
	)
}

// Plugin returns the pure-Go component family without global registration.
func Plugin() plugin.Definition {
	descriptor := plugin.Descriptor{
		DisplayName: "Linear PCM",
		Version:     "0.1.0",
		License:     "MIT",
		Build:       plugin.BuildModePureGo,
	}
	return plugin.Define[pluginID](descriptor,
		newComponent[readerID](readerOperation, "Raw PCM reader"),
		newComponent[parserID](parserOperation, "Linear PCM parser"),
		newComponent[decoderID](decoderOperation, "Linear PCM decoder"),
		newComponent[encoderID](encoderOperation, "Linear PCM encoder"),
		newComponent[writerID](writerOperation, "Raw PCM writer"),
	)
}

// Set returns the self-contained composition, including sample property and
// codec/parser declarations.
func Set() plugin.Set {
	result := plugin.NewSet(Plugin()).AddDeclaration(Binding())
	for _, declaration := range sample.Declarations() {
		result = result.AddDeclaration(declaration)
	}
	for _, declaration := range codec.Declarations() {
		result = result.AddDeclaration(declaration)
	}
	return result
}
