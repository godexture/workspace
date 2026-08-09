// Package wave implements RIFF/RF64 WAVE framing on the typed component stack.
package wave

import (
	"github.com/godexture/godec/media/format"
	"github.com/godexture/godec/plugin"
)

type (
	pluginID  struct{}
	demuxerID struct{}
	waveID    struct{}
)

func DemuxerIdentity() plugin.Identity { return plugin.IdentityOf[demuxerID]() }

func WAVE() format.Format {
	value, err := format.Define[waveID](nil)
	if err != nil {
		panic(err)
	}
	return value
}

// Plugin returns the pure-Go WAVE component family.
func Plugin() plugin.Definition {
	return plugin.Define[pluginID](plugin.Descriptor{
		DisplayName: "WAVE",
		Version:     "0.1.0",
		License:     "MIT",
		Build:       plugin.BuildModePureGo,
	}, demuxerComponent())
}
