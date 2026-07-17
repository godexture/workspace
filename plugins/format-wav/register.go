package wav

import (
	"fmt"
	"io"

	godec "github.com/godexture/core"
	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
	internal "github.com/godexture/format-wav/internal"
	engine "github.com/godexture/sdk/engine"
)

func Probe(r io.Reader) manifest.ProbeScore {
	return internal.Probe(r)
}

func NewDemuxer(r io.ReadSeeker, config DemuxerConfig) (*internal.Demuxer, error) {
	return internal.NewDemuxer(r, config.ApplyDefaults())
}

func NewMuxer(w io.Writer, config MuxerConfig) *internal.Muxer {
	return internal.NewMuxer(w, config.ApplyDefaults())
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
			Name:        "wav-demuxer",
			Description: "WAV demuxer",
		},
		Probe: Probe,
		Factory: func(r io.Reader, config registry.Configuration) (node.Demuxer, error) {
			rs, ok := r.(io.ReadSeeker)
			if !ok {
				return nil, fmt.Errorf("format-wav demuxer requires io.ReadSeeker")
			}

			resolved := engine.ResolveConfig[DemuxerConfig](config)
			dmx, err := internal.NewDemuxer(rs, resolved)
			if err != nil {
				return nil, err
			}
			return engine.WrapDemuxer(dmx), nil
		},
	}); err != nil {
		panic(err)
	}

	if err := godec.Register(MuxerConfig{}, registry.MuxerManifest{
		BaseManifest: registry.BaseManifest{
			Name:        "wav-muxer",
			Description: "WAV muxer",
		},
		Factory: func(w io.Writer, cfg registry.Configuration) (node.Muxer, error) {
			resolved := engine.ResolveConfig[MuxerConfig](cfg)
			mux := internal.NewMuxer(w, resolved)
			return engine.WrapMuxer(mux), nil
		},
	}); err != nil {
		panic(err)
	}
}
