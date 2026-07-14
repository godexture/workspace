package bridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"

	flacCodec "github.com/godexture/codec-flac"
	pcmCodec "github.com/godexture/codec-pcm"
	"github.com/godexture/core/domain/media"
	flacFormat "github.com/godexture/format-flac"
	wavFormat "github.com/godexture/format-wav"
	"github.com/godexture/sdk/engine"
	"github.com/godexture/sdk/testutil"
)

func Decode(flacBytes []byte) ([]float32, error) {
	demuxer, stream, err := newDemuxerWithAudioStream(flacBytes)
	if err != nil {
		return nil, err
	}

	decoder := flacCodec.NewDecoderEngine(flacCodec.NewDecoderConfigFromStreamInfo(stream))
	return testutil.DecodeToFloat32(context.Background(), demuxer, decoder)
}

func AudioAttributes(flacBytes []byte) (media.AudioAttributes, error) {
	_, stream, err := newDemuxerWithAudioStream(flacBytes)
	if err != nil {
		return media.AudioAttributes{}, err
	}
	return stream.Audio, nil
}

func EncodeFLAC(pcm []float32, attrs media.AudioAttributes) ([]byte, error) {
	buf := testutil.NewBuffer(nil)
	muxer := flacFormat.NewMuxerEngine(buf)
	stream := media.StreamInfo{
		Type:            media.MediaAudio,
		MediaAttributes: media.MediaAttributes{Codec: media.CodecFLAC, Audio: attrs},
	}
	if _, err := muxer.AddStream(stream); err != nil {
		return nil, err
	}

	// Keep this integration test quick while still exercising the complete
	// encoder path. The conformance and internal tests cover the higher-order
	// predictor and partition-search combinations separately.
	encoder := flacCodec.NewEncoderEngine(flacCodec.EncoderConfig{
		BlockSize:             4096,
		MaxFixedOrder:         0,
		MaxLPCOrder:           0,
		MaxRicePartitionOrder: 0,
		StreamableSubset:      true,
	})
	if err := testutil.EncodeToMuxer(context.Background(), encoder, muxer, pcm, attrs); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func newDemuxerWithAudioStream(flacBytes []byte) (engine.DemuxerEngine, media.StreamInfo, error) {
	demuxer, err := flacFormat.NewDemuxerEngine(bytes.NewReader(flacBytes))
	if err != nil {
		return nil, media.StreamInfo{}, err
	}

	streams, _, err := demuxer.Analyze()
	if err != nil {
		return nil, media.StreamInfo{}, err
	}
	if len(streams) == 0 {
		return nil, media.StreamInfo{}, fmt.Errorf("no streams found in FLAC")
	}
	return demuxer, streams[0], nil
}

func EncodeWAV(pcm []float32, attrs media.AudioAttributes) ([]byte, error) {
	buf := testutil.NewBuffer(nil)
	muxer := wavFormat.NewMuxerEngine(buf)

	stream := media.StreamInfo{
		Type:            media.MediaAudio,
		MediaAttributes: media.MediaAttributes{Codec: media.CodecLPCM, Audio: attrs},
	}
	if _, err := muxer.AddStream(stream); err != nil {
		return nil, err
	}

	encoder := pcmCodec.NewEncoderEngine(pcmCodec.EncoderConfig{
		CodecID:   media.CodecLPCM,
		ByteOrder: binary.LittleEndian,
	})
	if err := testutil.EncodeToMuxer(context.Background(), encoder, muxer, pcm, attrs); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
