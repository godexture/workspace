// Package id3 implements standalone ID3 metadata encodings.
package id3

import (
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/plugin"
)

type (
	pluginID struct{}
	v1ID     struct{}
	v2ID     struct{}
	configID struct{}
)

type configuration struct{}

func configurationSchema() config.Schema[configuration] {
	return config.Struct[configID](func() configuration { return configuration{} }).Version("1").Build()
}

// V1EncodingIdentity identifies the standalone ID3v1 metadata encoding.
func V1EncodingIdentity() plugin.Identity { return plugin.IdentityOf[v1ID]() }

// V2EncodingIdentity identifies the standalone ID3v2 metadata encoding.
func V2EncodingIdentity() plugin.Identity { return plugin.IdentityOf[v2ID]() }

// Plugin returns the pure-Go ID3 metadata family. MP3 owns the carrier and
// binds it in a later composition; this encoding remains format-independent.
func Plugin() plugin.Definition {
	definition := plugin.Define[pluginID](plugin.Descriptor{
		DisplayName: "ID3 metadata",
		Version:     "0.1.0",
		License:     "MIT",
		Build:       plugin.BuildModePureGo,
	}, v1Component(), v2Component())
	return definition.WithDeclarations(tag.Declarations()...)
}

// Set returns the complete standalone ID3 composition.
func Set() plugin.Set { return plugin.NewSet(Plugin()) }

func v1Component() plugin.Component {
	return plugin.NewComponent[v1ID](plugin.Descriptor{DisplayName: "ID3v1 metadata encoding"}, configurationSchema(),
		metadata.WithEncoding(parseV1, marshalV1,
			tag.Title().Erased(),
			tag.Artist().Erased(),
			tag.Album().Erased(),
			tag.Date().Erased(),
			tag.Genre().Erased(),
			tag.Comment().Erased(),
			tag.TrackNumber().Erased(),
		),
	)
}

func v2Component() plugin.Component {
	return plugin.NewComponent[v2ID](plugin.Descriptor{DisplayName: "ID3v2 metadata encoding"}, configurationSchema(), metadata.WithEncoding(parseV2, marshalV2,
		tag.Title().Erased(),
		tag.Artist().Erased(),
		tag.Album().Erased(),
		tag.Date().Erased(),
		tag.Genre().Erased(),
		tag.Comment().Erased(),
		tag.Composer().Erased(),
		tag.Lyrics().Erased(),
		tag.Website().Erased(),
		tag.TrackNumber().Erased(),
		tag.TotalTracks().Erased(),
		tag.DiscNumber().Erased(),
		tag.TotalDiscs().Erased(),
		tag.Copyright().Erased(),
		tag.Encoder().Erased(),
		tag.Picture().Erased(),
	))
}
