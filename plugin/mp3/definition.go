// Package mp3 implements MP3 elementary-stream framing on the typed component
// stack. Decoder and writer components are deliberately separate surfaces.
package mp3

import (
	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/codec"
	"github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plugin"
)

type (
	pluginID        struct{}
	readerID        struct{}
	parserID        struct{}
	formatID        struct{}
	leadingID3v2ID  struct{}
	trailingID3v1ID struct{}
	configID        struct{}
)

// ReaderIdentity identifies the MP3 elementary-stream reader.
func ReaderIdentity() plugin.Identity { return plugin.IdentityOf[readerID]() }

// ParserIdentity identifies the MP3 frame packetizer.
func ParserIdentity() plugin.Identity { return plugin.IdentityOf[parserID]() }

// LeadingID3v2 identifies an ID3v2 tag area at the beginning of an MP3
// stream. The MP3 owner declares the carrier; an ID3 encoding binds to it in
// a composition.
func LeadingID3v2() carrier.ID { return carrier.Define[leadingID3v2ID]() }

// TrailingID3v1 identifies the optional 128-byte ID3v1 tag at stream end.
func TrailingID3v1() carrier.ID { return carrier.Define[trailingID3v1ID]() }

// MP3 identifies an MP3 elementary stream.
func MP3() format.Format {
	value, err := format.Define[formatID]([]carrier.ID{LeadingID3v2(), TrailingID3v1()}, format.WithExtensions("mp3"))
	if err != nil {
		panic(err)
	}
	return value
}

// CodecTag names the MP3 coded stream.
func CodecTag() format.Tag {
	return format.NewTag("mp3", "audio")
}

// Plugin returns the MP3 framing family. Metadata encodings and audio codecs
// are independent plugins connected by composition bindings.
func Plugin() plugin.Definition {
	definition := plugin.Define[pluginID](plugin.Descriptor{
		DisplayName: "MP3",
		Version:     "0.1.0",
		License:     "MIT",
		Build:       plugin.BuildModePureGo,
	}, readerComponent(), parserComponent())
	declarations := append(codec.Declarations(), sample.Declarations()...)
	declarations = append(declarations, stream.Declarations()...)
	declarations = append(declarations, framingDeclaration())
	return definition.WithDeclarations(declarations...)
}

// Set returns the self-contained MP3 framing composition.
func Set() plugin.Set { return plugin.NewSet(Plugin()) }
