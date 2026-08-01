package flac

import (
	"fmt"
	"io"

	godec "github.com/godexture/godec/core"
	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/node"
	"github.com/godexture/godec/core/registry"
	internal "github.com/godexture/godec/plugin/flac/internal/format"
	engine "github.com/godexture/godec/sdk/engine"
)

var (
	Probe = internal.Probe

	NewDemuxer = internal.NewDemuxer
	NewMuxer   = internal.NewMuxer
)

func NewDemuxerEngine(r io.ReadSeeker, cfg DemuxerConfig) (engine.DemuxerEngine, error) {
	resolved, err := engine.ResolveConfig[internal.DemuxerConfig, DemuxerConfig](cfg)
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
	if err := godec.Register(registry.DemuxerManifest{
		BaseManifest: registry.BaseManifest{
			Name:                 "flac",
			Description:          "FLAC demuxer",
			ConfigurationFactory: registry.NewConfigurationFactory(NewDemuxerConfig),
		},
		Probe: Probe,
		Factory: func(r io.Reader, config registry.Configuration) (node.Demuxer, error) {
			rs, ok := r.(io.ReadSeeker)
			if !ok {
				return nil, fmt.Errorf("format-flac demuxer requires io.ReadSeeker")
			}

			resolved, err := engine.ResolveConfig[internal.DemuxerConfig, DemuxerConfig](config)
			if err != nil {
				return nil, err
			}
			demuxer, err := internal.NewDemuxer(rs, resolved)
			if err != nil {
				return nil, err
			}
			return engine.WrapDemuxer(demuxer), nil
		},
	}); err != nil {
		panic(err)
	}

	if err := godec.Register(registry.MuxerManifest{
		BaseManifest: registry.BaseManifest{
			Name:                 "flac",
			Description:          "FLAC muxer",
			ConfigurationFactory: registry.NewConfigurationFactory(NewMuxerConfig),
		},
		Extensions:   []string{".flac"},
		Codecs:       []media.CodecID{media.CodecFLAC},
		DefaultCodec: media.CodecFLAC,
		Factory: func(w io.Writer, config registry.Configuration) (node.Muxer, error) {
			resolved, err := engine.ResolveConfig[internal.MuxerConfig, MuxerConfig](config)
			if err != nil {
				return nil, err
			}
			return engine.WrapMuxer(internal.NewMuxer(w, resolved)), nil
		},
	}); err != nil {
		panic(err)
	}
}
