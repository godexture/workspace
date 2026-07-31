package test

import (
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
	t.Parallel()
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
				Demux: func(r io.ReadSeeker) (engine.DemuxerEngine, error) {
					return wavFormat.NewDemuxerEngine(r, wavFormat.MustNewDemuxerConfig())
				},
				Decode: func(stream media.StreamInfo) engine.DecoderEngine {
					decoder, err := pcmCodec.NewDecoderEngine(stream, pcmCodec.MustNewDecoderConfig())
					if err != nil {
						t.Fatal(err)
					}
					return decoder
				},
				Encode: func() engine.EncoderEngine {
					return pcmCodec.NewEncoderEngine(
						media.StreamInfo{},
						pcmCodec.MustNewEncoderConfig(
							pcmCodec.WithCodecID(profile.Codec),
							pcmCodec.WithADPCM(profile.ADPCM),
						))
				},
				Mux: func(w io.Writer) engine.MuxerEngine {
					mux, err := wavFormat.NewMuxerEngine(w, wavFormat.MustNewMuxerConfig())
					if err != nil {
						t.Fatal(err)
					}
					return mux
				},
			})
		})
	}
}
