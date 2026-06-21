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

type Config struct {
	ForceRF64 bool
}

func (Config) NodeConfiguration() {}

func Probe(r io.Reader) manifest.ProbeScore {
	return internal.Probe(r)
}

func NewDemuxer(r io.ReadSeeker) (*internal.Demuxer, error) {
	return internal.NewDemuxer(r)
}

func NewMuxer(w io.Writer) *internal.Muxer {
	return internal.NewMuxer(w)
}

func NewDemuxerEngine(r io.ReadSeeker) (engine.DemuxerEngine, error) {
	return internal.NewDemuxer(r)
}

func NewMuxerEngine(w io.Writer) engine.MuxerEngine {
	return internal.NewMuxer(w)
}

func init() {
	if err := godec.Register(Config{}, registry.DemuxerManifest{
		BaseManifest: registry.BaseManifest{
			Name:        "wav-demuxer",
			Description: "WAV demuxer",
		},
		Probe: Probe,
		Factory: func(r io.Reader, _ registry.Configuration) (node.Demuxer, error) {
			rs, ok := r.(io.ReadSeeker)
			if !ok {
				return nil, fmt.Errorf("format-wav demuxer requires io.ReadSeeker")
			}

			dmx, err := NewDemuxerEngine(rs)
			if err != nil {
				return nil, err
			}
			return engine.WrapDemuxer(dmx), nil
		},
	}); err != nil {
		panic(err)
	}

	if err := godec.Register(Config{}, registry.MuxerManifest{
		BaseManifest: registry.BaseManifest{
			Name:        "wav-muxer",
			Description: "WAV muxer",
		},
		Factory: func(w io.Writer, cfg registry.Configuration) (node.Muxer, error) {
			forceRF64 := false
			if cfg != nil {
				if wavCfg, ok := cfg.(Config); ok {
					forceRF64 = wavCfg.ForceRF64
				} else if wavCfgPtr, ok := cfg.(*Config); ok && wavCfgPtr != nil {
					forceRF64 = wavCfgPtr.ForceRF64
				}
			}
			mux := NewMuxer(w)
			mux.ForceRF64 = forceRF64
			return engine.WrapMuxer(mux), nil
		},
	}); err != nil {
		panic(err)
	}
}
