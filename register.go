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

// Config はformat-mp3プラグインの設定型。キーとして使われる。
type Config struct{}

func (Config) NodeConfiguration() {}

func Probe(r io.Reader) manifest.ProbeScore {
	return internal.Probe(r)
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
			Name:        "mp3-demuxer",
			Description: "MP3 demuxer (format-mp3 plugin)",
		},
		Probe: Probe,
		Factory: func(r io.Reader, _ registry.Configuration) (node.Demuxer, error) {
			rs, ok := r.(io.ReadSeeker)
			if !ok {
				return nil, fmt.Errorf("format-mp3 demuxer requires io.ReadSeeker")
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

	if err := godec.Register(Config{}, registry.MuxerManifest{
		BaseManifest: registry.BaseManifest{
			Name:        "mp3-muxer",
			Description: "MP3 muxer (format-mp3 plugin)",
		},
		Factory: func(w io.Writer, _ registry.Configuration) (node.Muxer, error) {
			return engine.WrapMuxer(NewMuxerEngine(w)), nil
		},
	}); err != nil {
		panic(err)
	}
}
