package wav

import (
	"fmt"
	"io"

	godec "github.com/godexture/core"
	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
	internal "github.com/godexture/format-wav/internal"
	engine "github.com/godexture/sdk/engine"
)

var (
	Probe = internal.Probe

	NewDemuxer = internal.NewDemuxer
	NewMuxer   = internal.NewMuxer
)

func NewDemuxerEngine(r io.ReadSeeker, config DemuxerConfig) (engine.DemuxerEngine, error) {
	resolved, err := engine.ResolveConfig[internal.DemuxerConfig, DemuxerConfig](config)
	if err != nil {
		return nil, err
	}
	return internal.NewDemuxer(r, resolved)
}

func NewMuxerEngine(w io.Writer, config MuxerConfig) (engine.MuxerEngine, error) {
	resolved, err := engine.ResolveConfig[internal.MuxerConfig, MuxerConfig](config)
	if err != nil {
		return nil, err
	}
	return internal.NewMuxer(w, resolved), nil
}

func init() {
	if err := godec.Register(NewDemuxerConfig(), registry.DemuxerManifest{
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

			resolved, err := engine.ResolveConfig[internal.DemuxerConfig, DemuxerConfig](config)
			if err != nil {
				return nil, err
			}
			dmx, err := internal.NewDemuxer(rs, resolved)
			if err != nil {
				return nil, err
			}
			return engine.WrapDemuxer(dmx), nil
		},
	}); err != nil {
		panic(err)
	}

	if err := godec.Register(NewMuxerConfig(), registry.MuxerManifest{
		BaseManifest: registry.BaseManifest{
			Name:        "wav-muxer",
			Description: "WAV muxer",
		},
		Factory: func(w io.Writer, cfg registry.Configuration) (node.Muxer, error) {
			resolved, err := engine.ResolveConfig[internal.MuxerConfig, MuxerConfig](cfg)
			if err != nil {
				return nil, err
			}
			mux := internal.NewMuxer(w, resolved)
			return engine.WrapMuxer(mux), nil
		},
	}); err != nil {
		panic(err)
	}
}
