// Package acme is an out-of-tree-style plugin fixture implemented only with
// godec's public extension contracts.
package acme

import (
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/media/codec"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/plugin"
)

type (
	pluginID   struct{}
	configID   struct{}
	sourceID   struct{}
	readerID   struct{}
	decoderID  struct{}
	writerID   struct{}
	encodingID struct{}
)

type configuration struct{}

func configurationSchema() config.Schema[configuration] {
	return config.Struct[configID](func() configuration { return configuration{} }).Version("1").Build()
}

func SourceIdentity() plugin.Identity   { return plugin.IdentityOf[sourceID]() }
func ReaderIdentity() plugin.Identity   { return plugin.IdentityOf[readerID]() }
func DecoderIdentity() plugin.Identity  { return plugin.IdentityOf[decoderID]() }
func WriterIdentity() plugin.Identity   { return plugin.IdentityOf[writerID]() }
func EncodingIdentity() plugin.Identity { return plugin.IdentityOf[encodingID]() }

// Plugin returns a self-contained Provider, Format, Codec, metadata Encoding,
// schema, and their owned declarations.
func Plugin() plugin.Definition {
	definition := plugin.Define[pluginID](plugin.Descriptor{
		DisplayName: "ACME integration fixture",
		Version:     "1.0.0",
		License:     "MIT",
		Build:       plugin.BuildModePureGo,
	}, sourceComponent(), readerComponent(), decoderComponent(), writerComponent(), encodingComponent())
	declarations := []plugin.Declaration{
		codec.BindWithoutParser(CodecTag(), codec.New(DecoderIdentity())),
		metadata.Bind(LabelCarrier(), EncodingIdentity()),
		plugin.DeclareKey(Label()),
	}
	return definition.WithDeclarations(declarations...)
}
