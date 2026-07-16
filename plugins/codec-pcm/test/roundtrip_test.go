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
				Decode: func(stream media.StreamInfo) engine.DecoderEngine {
					return pcmCodec.NewDecoderEngine(stream, pcmCodec.DecoderConfig{})
				},
				Encode: func() engine.EncoderEngine {
					return pcmCodec.NewEncoderEngine(
						media.StreamInfo{},
						pcmCodec.NewEncoderConfig(
							pcmCodec.WithCodecID(profile.Codec),
							pcmCodec.WithByteOrder(binary.LittleEndian),
							pcmCodec.WithADPCM(profile.ADPCM),
						))
				},
				Mux: func(w io.Writer) engine.MuxerEngine {
					return wavFormat.NewMuxerEngine(w, wavFormat.MuxerConfig{})
				},
			})
		})
	}
}
