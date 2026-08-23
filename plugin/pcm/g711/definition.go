// Package g711 implements the A-law and mu-law companded codecs on the typed
// component stack. A companded stream states a signal and no storage
// representation, so its samples become readable only here.
package g711

import (
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/plugin"
)

type (
	pluginID      struct{}
	parserID      struct{}
	aLawDecoderID struct{}
	uLawDecoderID struct{}
	aLawEncoderID struct{}
	uLawEncoderID struct{}
)

// ParserIdentity returns the component that cuts a companded byte stream into
// packets. Both laws store one byte per sample, so they share it.
func ParserIdentity() plugin.Identity { return plugin.IdentityOf[parserID]() }

// DecoderIdentity and EncoderIdentity return the components for one companding
// law. The law is part of the component rather than its configuration, because
// a composition binds a container tag to a component and cannot carry config.
func DecoderIdentity(law Law) plugin.Identity {
	if law == ALaw {
		return plugin.IdentityOf[aLawDecoderID]()
	}
	return plugin.IdentityOf[uLawDecoderID]()
}

func EncoderIdentity(law Law) plugin.Identity {
	if law == ALaw {
		return plugin.IdentityOf[aLawEncoderID]()
	}
	return plugin.IdentityOf[uLawEncoderID]()
}

// Plugin returns the pure-Go companded codec family.
func Plugin() plugin.Definition {
	definition := plugin.Define[pluginID](plugin.Descriptor{
		DisplayName: "G.711",
		Version:     "0.1.0",
		License:     "MIT",
		Build:       plugin.BuildModePureGo,
	},
		newParser[parserID](),
		newCodec[aLawDecoderID](ALaw, decoderOperation, "A-law decoder"),
		newCodec[uLawDecoderID](ULaw, decoderOperation, "mu-law decoder"),
		newCodec[aLawEncoderID](ALaw, encoderOperation, "A-law encoder"),
		newCodec[uLawEncoderID](ULaw, encoderOperation, "mu-law encoder"),
	)
	return definition.WithDeclarations(sample.Declarations()...)
}

// Set returns the self-contained composition.
func Set() plugin.Set { return plugin.NewSet(Plugin()) }
