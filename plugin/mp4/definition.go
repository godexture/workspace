package mp4

import (
	"github.com/godexture/godec/media/codec"
	"github.com/godexture/godec/media/format"
	mediasample "github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plugin"
)

type (
	pluginID  struct{}
	demuxerID struct{}
	muxerID   struct{}
	formatID  struct{}
)

// DemuxerIdentity identifies the ISO BMFF packet reader.
func DemuxerIdentity() plugin.Identity { return plugin.IdentityOf[demuxerID]() }

// MuxerIdentity identifies the ISO BMFF same-format remuxer.
func MuxerIdentity() plugin.Identity { return plugin.IdentityOf[muxerID]() }

// MP4 identifies ISO Base Media File Format streams carried as MP4 files.
func MP4() format.Format {
	value, err := format.DefinePacketized[formatID](nil, format.WithExtensions("mp4"))
	if err != nil {
		panic(err)
	}
	return value
}

// SampleEntryTag identifies one ISO BMFF sample-entry four-character code.
// Codec plugins use it when they declare a binding for packets demuxed from
// this container.
func SampleEntryTag(value string) format.Tag {
	if len(value) != 4 {
		return ""
	}
	return format.NewTag("mp4", value)
}

// Plugin returns the pure-Go ISO BMFF reader family.
func Plugin() plugin.Definition {
	definition := plugin.Define[pluginID](plugin.Descriptor{
		DisplayName: "MP4",
		Version:     "0.1.0",
		License:     "MIT",
		Build:       plugin.BuildModePureGo,
	}, demuxerComponent(), muxerComponent())
	return definition.WithDeclarations(append(codec.Declarations(), stream.Declarations()...)...)
}

// Set returns the self-contained MP4 composition.
func Set() plugin.Set { return plugin.NewSet(Plugin()) }

// SampleEntryCodings lists the sample entries this reader describes as linear
// PCM, with the coding each one stores. A composition binds them to the codec
// components that read those codings; the two families never import each other.
func SampleEntryCodings() map[string]mediasample.Coding {
	result := make(map[string]mediasample.Coding, len(linearPCMEntries))
	for entry, description := range linearPCMEntries {
		result[string(entry[:])] = description.Coding
	}
	return result
}
