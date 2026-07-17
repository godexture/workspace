package mp3

import (
	"fmt"
	"io"

	godec "github.com/godexture/core"
	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
	internal "github.com/godexture/format-mp3/internal"
	engine "github.com/godexture/sdk/engine"
)

func Probe(r io.Reader) manifest.ProbeScore {
	return internal.Probe(r)
}

func NewDemuxerEngine(r io.ReadSeeker, config DemuxerConfig) (engine.DemuxerEngine, error) {
	return internal.NewDemuxer(r, config.ApplyDefaults())
}

func NewMuxerEngine(w io.Writer, config MuxerConfig) engine.MuxerEngine {
	return internal.NewMuxer(w, config.ApplyDefaults())
}

func init() {
	if err := godec.Register(DemuxerConfig{}, registry.DemuxerManifest{
		BaseManifest: registry.BaseManifest{
			Name:        "mp3-demuxer",
			Description: "MP3 demuxer (format-mp3 plugin)",
		},
		Probe: Probe,
		Factory: func(r io.Reader, config registry.Configuration) (node.Demuxer, error) {
			rs, ok := r.(io.ReadSeeker)
			if !ok {
				return nil, fmt.Errorf("format-mp3 demuxer requires io.ReadSeeker")
			}
			resolved := engine.ResolveConfig[DemuxerConfig](config)
			demuxer, err := internal.NewDemuxer(rs, resolved)
			if err != nil {
				return nil, err
			}
			return engine.WrapDemuxer(demuxer), nil
		},
	}); err != nil {
		panic(err)
	}

	if err := godec.Register(MuxerConfig{}, registry.MuxerManifest{
		BaseManifest: registry.BaseManifest{
			Name:        "mp3-muxer",
			Description: "MP3 muxer (format-mp3 plugin)",
		},
		Factory: func(w io.Writer, config registry.Configuration) (node.Muxer, error) {
			resolved := engine.ResolveConfig[MuxerConfig](config)
			return engine.WrapMuxer(internal.NewMuxer(w, resolved)), nil
		},
	}); err != nil {
		panic(err)
	}
}
