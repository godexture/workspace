//go:generate go run ./snapshot-generator .

package test

import (
	"io"
	"testing"

	"github.com/godexture/godec/core/domain/media"
	pcmCodec "github.com/godexture/godec/plugin/pcm"
	"github.com/godexture/godec/plugin/pcm/test/config"
	wavFormat "github.com/godexture/godec/plugin/wave"
	"github.com/godexture/godec/sdk/engine"
	"github.com/godexture/godec/sdk/testutil"
)

func TestSnapshots(t *testing.T) {
	t.Parallel()
	for _, profile := range config.Profiles {
		t.Run(profile.Name, func(t *testing.T) {
			t.Parallel()

			dataPath := config.BuildTestdataPath(profile.Name)

			testutil.RunSnapshotTests(t, testutil.SnapshotConfig{
				MediaPath: dataPath,
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
