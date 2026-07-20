package mp3

import (
	"fmt"
	"io"

	godec "github.com/godexture/core"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
	internal "github.com/godexture/format-mp3/internal"
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
	if err := godec.Register(registry.DemuxerManifest{
		BaseManifest: registry.BaseManifest{
			Name:                 "mp3",
			Description:          "MP3 demuxer (format-mp3 plugin)",
			ConfigurationFactory: registry.NewConfigurationFactory(NewDemuxerConfig),
		},
		Probe: Probe,
		Factory: func(r io.Reader, config registry.Configuration) (node.Demuxer, error) {
			rs, ok := r.(io.ReadSeeker)
			if !ok {
				return nil, fmt.Errorf("format-mp3 demuxer requires io.ReadSeeker")
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
			Name:                 "mp3",
			Description:          "MP3 muxer (format-mp3 plugin)",
			ConfigurationFactory: registry.NewConfigurationFactory(NewMuxerConfig),
		},
		Extensions:   []string{".mp3"},
		Codecs:       []media.CodecID{media.CodecMP3},
		DefaultCodec: media.CodecMP3,
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
