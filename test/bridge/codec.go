package bridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/testutil"

	pcmCodec "github.com/godexture/codec-pcm"
	wavFormat "github.com/godexture/format-wav"
)

func Encode(pcm []float32, codec media.CodecID, attrs media.AudioAttributes) ([]byte, error) {
	encoder := pcmCodec.NewEncoderEngine(pcmCodec.EncoderConfig{
		CodecID:   codec,
		ByteOrder: binary.LittleEndian,
	})

	buf := testutil.NewBuffer(nil)
	muxer := wavFormat.NewMuxerEngine(buf)

	stream := media.StreamInfo{
		Type:            media.MediaAudio,
		MediaAttributes: media.MediaAttributes{Codec: codec, Audio: attrs},
	}
	if _, err := muxer.AddStream(stream); err != nil {
		return nil, err
	}

	if err := testutil.EncodeToMuxer(context.Background(), encoder, muxer, pcm, attrs); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func Decode(wavBytes []byte) ([]float32, error) {
	demuxer, err := wavFormat.NewDemuxerEngine(bytes.NewReader(wavBytes))
	if err != nil {
		return nil, err
	}

	streams, _, err := demuxer.Analyze()
	if err != nil {
		return nil, err
	}
	if len(streams) == 0 {
		return nil, fmt.Errorf("no streams found in WAV")
	}

	stream := streams[0]
	a := stream.Audio

	targetFormat := a.Format
	if stream.Codec != media.CodecLPCM {
		targetFormat = media.SampleFormatS16
	}

	cfg := pcmCodec.NewConfigWithAudio(a.SampleRate, targetFormat, a.ChannelLayout)
	cfg.CodecID = stream.Codec
	decoder := pcmCodec.NewDecoderEngine(cfg)

	return testutil.DecodeToFloat32(context.Background(), demuxer, decoder)
}
