// Package vorbiscomment implements standalone FLAC Vorbis Comment blocks.
package vorbiscomment

import (
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/plugin"
)

type (
	pluginID   struct{}
	encodingID struct{}
	configID   struct{}
)

type configuration struct{}

func configurationSchema() config.Schema[configuration] {
	return config.Struct[configID](func() configuration { return configuration{} }).Version("1").Build()
}

// EncodingIdentity identifies the standalone Vorbis Comment metadata encoding.
func EncodingIdentity() plugin.Identity { return plugin.IdentityOf[encodingID]() }

// Plugin returns the metadata-only Vorbis Comment family. FLAC and Ogg own
// their envelopes and bind this encoding from a composition.
func Plugin() plugin.Definition {
	definition := plugin.Define[pluginID](plugin.Descriptor{
		DisplayName: "Vorbis Comment metadata",
		Version:     "0.1.0",
		License:     "MIT",
		Build:       plugin.BuildModePureGo,
	}, component())
	return definition.WithDeclarations(tag.Declarations()...)
}

// Set returns the complete standalone Vorbis Comment composition.
func Set() plugin.Set { return plugin.NewSet(Plugin()) }

func component() plugin.Component {
	return plugin.NewComponent[encodingID](plugin.Descriptor{DisplayName: "Vorbis Comment metadata encoding"}, configurationSchema(),
		metadata.WithEncoding(parse, marshal,
			tag.Title().Erased(),
			tag.Artist().Erased(),
			tag.Album().Erased(),
			tag.Composer().Erased(),
			tag.Genre().Erased(),
			tag.Date().Erased(),
			tag.Comment().Erased(),
			tag.Copyright().Erased(),
			tag.License().Erased(),
			tag.Encoder().Erased(),
			tag.Lyrics().Erased(),
			tag.TrackNumber().Erased(),
			tag.TotalTracks().Erased(),
			tag.DiscNumber().Erased(),
			tag.TotalDiscs().Erased(),
			tag.Picture().Erased(),
		),
	)
}
