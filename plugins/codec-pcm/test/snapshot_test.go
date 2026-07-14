//go:generate go run ./snapshot-generator .

package test

import (
	"encoding/binary"
	"io"
	"testing"

	pcmCodec "github.com/godexture/codec-pcm"
	"github.com/godexture/codec-pcm/test/config"
	"github.com/godexture/core/domain/media"
	wavFormat "github.com/godexture/format-wav"
	"github.com/godexture/sdk/engine"
	"github.com/godexture/sdk/testutil"
)

func TestSnapshots(t *testing.T) {
	for _, profile := range config.Profiles {
		t.Run(profile.Name, func(t *testing.T) {
			t.Parallel()

			dataPath := config.BuildTestdataPath(profile.Name)

			testutil.RunSnapshotTests(t, testutil.SnapshotConfig{
				MediaPath: dataPath,
				Opts:      profile.CompareOptions,
				StreamInfo: &media.StreamInfo{
					Type:            media.MediaAudio,
					MediaAttributes: media.MediaAttributes{Codec: profile.Codec, Audio: profile.Attrs},
				},
				Demux: func(r io.ReadSeeker) (engine.DemuxerEngine, error) {
					return wavFormat.NewDemuxerEngine(r)
				},
				Decode: func(_ media.StreamInfo) engine.DecoderEngine {
					targetFormat := profile.Attrs.Format
					if profile.Codec != media.CodecLPCM {
						targetFormat = media.SampleFormatS16
					}
					cfg := pcmCodec.NewConfigWithAudio(profile.Attrs.SampleRate, targetFormat, profile.Attrs.ChannelLayout)
					cfg.CodecID = profile.Codec
					return pcmCodec.NewDecoderEngine(cfg)
				},
				Encode: func() engine.EncoderEngine {
					return pcmCodec.NewEncoderEngine(pcmCodec.EncoderConfig{
						CodecID:   profile.Codec,
						ByteOrder: binary.LittleEndian,
					})
				},
				Mux: func(buf *testutil.Buffer) engine.MuxerEngine { return wavFormat.NewMuxerEngine(buf) },
			})
		})
	}
}
