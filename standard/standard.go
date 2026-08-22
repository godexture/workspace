// Package standard composes the pure-Go plugins shipped with godec.
package standard

import (
	"github.com/godexture/godec/host"
	"github.com/godexture/godec/media/codec"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/plugin/file"
	"github.com/godexture/godec/plugin/mp4"
	"github.com/godexture/godec/plugin/pcm/linear"
	"github.com/godexture/godec/plugin/wave"
)

// Set returns the immutable official composition for file-backed MP4/WAVE and
// linear PCM processing.
func Set() plugin.Set {
	result := plugin.NewSet(file.Plugin(), linear.Plugin(), mp4.Plugin(), wave.Plugin()).
		AddDeclaration(codec.Bind(wave.PCMTag(), codec.New(linear.DecoderIdentity()), codec.NewParser(linear.ParserIdentity())))
	// MP4 carries linear PCM in already packetized sample entries, so these
	// bind the decoder without a parser. A planner only reaches for them when
	// copying the packets cannot satisfy the output.
	for _, entry := range []string{"sowt", "twos"} {
		result = result.AddDeclaration(codec.BindWithoutParser(mp4.SampleEntryTag(entry), codec.New(linear.DecoderIdentity())))
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
