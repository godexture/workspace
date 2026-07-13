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

type Config struct{}

func (Config) NodeConfiguration() {}

func Probe(r io.Reader) manifest.ProbeScore {
	return internal.Probe(r)
}

func NewDemuxer(r io.ReadSeeker) (*internal.Demuxer, error) {
	return internal.NewDemuxer(r)
}

func NewDemuxerEngine(r io.ReadSeeker) (engine.DemuxerEngine, error) {
	return internal.NewDemuxer(r)
}

func init() {
	if err := godec.Register(Config{}, registry.DemuxerManifest{
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
}
