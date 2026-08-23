// Package adpcm implements the two ADPCM variants a WAVE header can declare.
// Their samples are coded in blocks whose parameters the container carries
// without reading, so this family is what turns those blocks into samples.
package adpcm

import (
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/plugin/pcm/internal/adpcm/param"
)

type (
	pluginID          struct{}
	microsoftParserID struct{}
	imaParserID       struct{}
	microsoftID       struct{}
	imaID             struct{}
)

// Variant names one of the two block layouts. It belongs to the component
// rather than to its configuration, because a composition binds a container
// tag to a component and cannot carry configuration with it.
type Variant uint8

const (
	Microsoft Variant = iota + 1
	IMA
)

func (v Variant) Valid() bool { return v == Microsoft || v == IMA }

func (v Variant) String() string {
	switch v {
	case Microsoft:
		return "ms-adpcm"
	case IMA:
		return "ima-adpcm"
	default:
		return "unknown ADPCM variant"
	}
}

func (v Variant) kind() param.Kind {
	if v == Microsoft {
		return param.Microsoft
	}
	return param.IMA
}

// ParserIdentity returns the component that cuts a coded byte stream into
// whole blocks. Each variant sizes its blocks differently, so each has one.
func ParserIdentity(variant Variant) plugin.Identity {
	if variant == Microsoft {
		return plugin.IdentityOf[microsoftParserID]()
	}
	return plugin.IdentityOf[imaParserID]()
}

// DecoderIdentity returns the component that expands one variant into samples.
func DecoderIdentity(variant Variant) plugin.Identity {
	if variant == Microsoft {
		return plugin.IdentityOf[microsoftID]()
	}
	return plugin.IdentityOf[imaID]()
}

// Plugin returns the pure-Go ADPCM family. Encoding is not here yet: a newly
// built container has to state a block size before anything has coded a block,
// and nothing carries that number across a container boundary today.
func Plugin() plugin.Definition {
	definition := plugin.Define[pluginID](plugin.Descriptor{
		DisplayName: "ADPCM",
		Version:     "0.1.0",
		License:     "MIT",
		Build:       plugin.BuildModePureGo,
	},
		newParser[microsoftParserID](Microsoft, "Microsoft ADPCM parser"),
		newParser[imaParserID](IMA, "IMA ADPCM parser"),
		newDecoder[microsoftID](Microsoft, "Microsoft ADPCM decoder"),
		newDecoder[imaID](IMA, "IMA ADPCM decoder"),
	)
	return definition.WithDeclarations(sample.Declarations()...)
}

// Set returns the self-contained composition.
func Set() plugin.Set { return plugin.NewSet(Plugin()) }
