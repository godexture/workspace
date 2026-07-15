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
	"github.com/godexture/sdk/optional"
	"github.com/godexture/sdk/testutil"
)

func TestRoundtrip(t *testing.T) {
	for _, profile := range config.Profiles {
		t.Run(profile.Name, func(t *testing.T) {
			t.Parallel()

			wavPath := config.BuildTestdataPath(profile.Name)

			testutil.RunRoundtripTests(t, testutil.RoundtripConfig{
				MediaPath: wavPath,
				Opts:      profile.CompareOptions,
				StreamInfo: &media.StreamInfo{
					Type:            media.MediaAudio,
					MediaAttributes: media.MediaAttributes{Codec: profile.Codec, CodecParameters: profile.CodecParameters, Audio: profile.Attrs},
				},
				Demux: wavFormat.NewDemuxerEngine,
				Decode: func(_ media.StreamInfo) engine.DecoderEngine {
					targetFormat := profile.Attrs.Format
					if profile.Codec != media.CodecLPCM {
						targetFormat = media.SampleFormatS16
					}
					cfg := pcmCodec.NewConfigWithAudio(profile.Attrs.SampleRate, targetFormat, profile.Attrs.ChannelLayout)
					cfg.CodecID = optional.Some(profile.Codec)
					if profile.ADPCM.BlockAlign != 0 {
						cfg.ADPCM = optional.Some(profile.ADPCM)
					}
					return pcmCodec.NewDecoderEngine(cfg)
				},
				Encode: func() engine.EncoderEngine {
					return pcmCodec.NewEncoderEngine(pcmCodec.EncoderConfig{
						CodecID:   optional.Some(profile.Codec),
						ByteOrder: optional.Some[binary.ByteOrder](binary.LittleEndian),
						ADPCM:     optional.Some(profile.ADPCM),
					})
				},
				Mux: func(w io.Writer) engine.MuxerEngine {
					return wavFormat.NewMuxerEngine(w, wavFormat.MuxerConfig{})
				},
			})
		})
	}
}
