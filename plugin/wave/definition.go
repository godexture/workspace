// Package wave implements RIFF/RF64 WAVE framing on the typed component stack.
package wave

import (
	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/codec"
	"github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/plugin"
)

type (
	pluginID  struct{}
	demuxerID struct{}
	muxerID   struct{}
	infoID    struct{}
	waveID    struct{}
	infoSlot  struct{}
)

func DemuxerIdentity() plugin.Identity      { return plugin.IdentityOf[demuxerID]() }
func MuxerIdentity() plugin.Identity        { return plugin.IdentityOf[muxerID]() }
func InfoEncodingIdentity() plugin.Identity { return plugin.IdentityOf[infoID]() }

// RIFFInfo identifies a LIST/INFO metadata carrier inside WAVE.
func RIFFInfo() carrier.ID { return carrier.Define[infoSlot]() }

// PCMTag identifies the PCM codec tag carried by WAVE format headers.
func PCMTag() format.Tag { return format.NewTag("wave", "0001") }

func WAVE() format.Format {
	value, err := format.Define[waveID]([]carrier.ID{RIFFInfo()})
	if err != nil {
		panic(err)
	}
	return value
}

// Plugin returns the pure-Go WAVE component family.
func Plugin() plugin.Definition {
	definition := plugin.Define[pluginID](plugin.Descriptor{
		DisplayName: "WAVE",
		Version:     "0.1.0",
		License:     "MIT",
		Build:       plugin.BuildModePureGo,
	}, demuxerComponent(), muxerComponent(), infoComponent())
	declarations := []plugin.Declaration{InfoBinding()}
	declarations = append(declarations, tag.Declarations()...)
	declarations = append(declarations, sample.Declarations()...)
	declarations = append(declarations, codec.Declarations()...)
	return definition.WithDeclarations(declarations...)
}

// InfoBinding connects WAVE's LIST/INFO carrier to its standalone Encoding.
func InfoBinding() metadata.Binding { return metadata.Bind(RIFFInfo(), InfoEncodingIdentity()) }

// Set returns the self-contained WAVE composition and shared metadata keys.
func Set() plugin.Set {
	return plugin.NewSet(Plugin())
}
