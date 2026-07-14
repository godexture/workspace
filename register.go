package flac

import (
	"fmt"
	"io"

	godec "github.com/godexture/core"
	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
	internal "github.com/godexture/format-flac/internal"
	engine "github.com/godexture/sdk/engine"
)

type DemuxerConfig struct{}

func (DemuxerConfig) NodeConfiguration() {}

type MuxerConfig struct{}

func (MuxerConfig) NodeConfiguration() {}

func Probe(r io.Reader) manifest.ProbeScore {
	return internal.Probe(r)
}

func NewDemuxer(r io.ReadSeeker) (*internal.Demuxer, error) {
	return internal.NewDemuxer(r)
}

func NewDemuxerEngine(r io.ReadSeeker) (engine.DemuxerEngine, error) {
	return internal.NewDemuxer(r)
}

func NewMuxer(w io.Writer) *internal.Muxer {
	return internal.NewMuxer(w)
}

func NewMuxerEngine(w io.Writer) engine.MuxerEngine {
	return internal.NewMuxer(w)
}

func init() {
	if err := godec.Register(DemuxerConfig{}, registry.DemuxerManifest{
		BaseManifest: registry.BaseManifest{
			Name:        "flac-demuxer",
			Description: "FLAC demuxer",
		},
		Probe: Probe,
		Factory: func(r io.Reader, _ registry.Configuration) (node.Demuxer, error) {
			rs, ok := r.(io.ReadSeeker)
			if !ok {
				return nil, fmt.Errorf("format-flac demuxer requires io.ReadSeeker")
			}

			demuxer, err := NewDemuxerEngine(rs)
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
			Name:        "flac-muxer",
			Description: "FLAC muxer",
		},
		Factory: func(w io.Writer, _ registry.Configuration) (node.Muxer, error) {
			return engine.WrapMuxer(NewMuxerEngine(w)), nil
		},
	}); err != nil {
		panic(err)
	}
}
