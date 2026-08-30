// Package standard composes the pure-Go plugins shipped with godec.
package standard

import (
	"github.com/godexture/godec/host"
	"github.com/godexture/godec/media/codec"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/plugin/audio"
	"github.com/godexture/godec/plugin/file"
	"github.com/godexture/godec/plugin/id3"
	"github.com/godexture/godec/plugin/mp4"
	"github.com/godexture/godec/plugin/pcm/adpcm"
	"github.com/godexture/godec/plugin/pcm/g711"

	"github.com/godexture/godec/plugin/pcm/linear"
	"github.com/godexture/godec/plugin/vorbiscomment"
	"github.com/godexture/godec/plugin/wave"
)

// Set returns the immutable official composition for file-backed processing
// and standalone metadata encodings.
func Set() plugin.Set {
	result := plugin.NewSet(adpcm.Plugin(), audio.Plugin(), file.Plugin(), g711.Plugin(), id3.Plugin(), linear.Plugin(), mp4.Plugin(), vorbiscomment.Plugin(), wave.Plugin())
	// A container names a codec; the components that implement it live in
	// families that never import the container or each other. This is where
	// the two meet, and it is the only place that knows which component reads
	// a tag and which one writes it.
	for _, coding := range wave.Codings() {
		tag := wave.CodecTag(string(coding))
		result = result.
			AddDeclaration(codec.BindParser(tag, codec.NewParser(linear.ParserIdentity()))).
			AddDeclaration(codec.BindDecoder(tag, codec.New(linear.DecoderIdentity(coding)))).
			AddDeclaration(codec.BindEncoder(tag, codec.New(linear.EncoderIdentity(coding))))
	}
	// A stream whose samples are not stored one scalar each states no
	// representation a linear component could read, so its tag names the codec
	// that expands it instead.
	for _, law := range []g711.Law{g711.ALaw, g711.ULaw} {
		tag := wave.CodecTag(law.String())
		result = result.
			AddDeclaration(codec.BindParser(tag, codec.NewParser(g711.ParserIdentity()))).
			AddDeclaration(codec.BindDecoder(tag, codec.New(g711.DecoderIdentity(law)))).
			AddDeclaration(codec.BindEncoder(tag, codec.New(g711.EncoderIdentity(law))))
	}
	for _, variant := range []adpcm.Variant{adpcm.Microsoft, adpcm.IMA} {
		tag := wave.CodecTag(variant.String())
		result = result.
			AddDeclaration(codec.BindParser(tag, codec.NewParser(adpcm.ParserIdentity(variant)))).
			AddDeclaration(codec.BindDecoder(tag, codec.New(adpcm.DecoderIdentity(variant)))).
			AddDeclaration(codec.BindEncoder(tag, codec.New(adpcm.EncoderIdentity(variant))))
	}
	// MP4 carries linear PCM in already packetized sample entries, so these
	// name no parser. A planner only reaches for them when copying the packets
	// cannot satisfy the output.
	for entry, coding := range mp4.SampleEntryCodings() {
		result = result.AddDeclaration(codec.BindDecoder(mp4.SampleEntryTag(entry), codec.New(linear.DecoderIdentity(coding))))
	}
	return result
}

// NewHost builds the official composition with optional third-party plugin
// definitions. Every definition is carried by the same plugin.Set regardless
// of which traits it contributes.
func NewHost(extra ...plugin.Definition) (*host.Host, error) {
	set := Set()
	for _, definition := range extra {
		set = set.Add(definition)
	}
	return host.New(host.Plugins(set))
}
