package test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"

	pcmCodec "github.com/godexture/codec-pcm"
	"github.com/godexture/core/domain/media"
	wavFormat "github.com/godexture/format-wav"
	"github.com/godexture/sdk/engine"
	"github.com/godexture/sdk/testutil"
)

type codecConfig struct {
	name          string
	codecID       media.CodecID
	sampleRate    int
	channelLayout media.ChannelLayout
	format        media.SampleFormat
	maxDiff       float32 // Error tolerance limit for snapshot/roundtrip
}

var configs = []codecConfig{
	{name: "lpcm", codecID: media.CodecLPCM, sampleRate: 16000, channelLayout: media.LayoutStereo2_0, format: media.SampleFormatS16, maxDiff: 1e-4},
	{name: "pcmu", codecID: media.CodecPCMU, sampleRate: 8000, channelLayout: media.LayoutMono1, format: media.SampleFormatS16, maxDiff: 0.025},
	{name: "pcma", codecID: media.CodecPCMA, sampleRate: 8000, channelLayout: media.LayoutMono1, format: media.SampleFormatS16, maxDiff: 0.025},
	{name: "msadpcm", codecID: media.CodecMSADPCM, sampleRate: 8000, channelLayout: media.LayoutStereo2_0, format: media.SampleFormatS16, maxDiff: 0.15},
	{name: "imaadpcm", codecID: media.CodecIMAADPCM, sampleRate: 8000, channelLayout: media.LayoutStereo2_0, format: media.SampleFormatS16, maxDiff: 0.45},
}

func generateSineWave(sampleRate int, channels int, duration float64) []float32 {
	numSamples := int(float64(sampleRate) * duration)
	pcm := make([]float32, numSamples*channels)
	for i := 0; i < numSamples; i++ {
		t := float64(i) / float64(sampleRate)
		v1 := float32(math.Sin(2*math.Pi*100.0*t) * 0.5)
		pcm[i*channels] = v1
		if channels > 1 {
			v2 := float32(math.Sin(2*math.Pi*200.0*t) * 0.5)
			pcm[i*channels+1] = v2
		}
	}
	return pcm
}

func downmixToMono(stereo []float32) []float32 {
	mono := make([]float32, len(stereo)/2)
	for i := 0; i < len(mono); i++ {
		mono[i] = (stereo[i*2] + stereo[i*2+1]) * 0.5
	}
	return mono
}

func resample16kTo8k(in []float32, channels int) []float32 {
	out := make([]float32, len(in)/2)
	for i := 0; i < len(out)/channels; i++ {
		for c := 0; c < channels; c++ {
			out[i*channels+c] = in[i*2*channels+c]
		}
	}
	return out
}

func createAudioFrame(pcm []float32, format media.SampleFormat, layout media.ChannelLayout, sampleRate int) (*media.Frame, error) {
	channels := layout.ChannelCount()
	samples := len(pcm) / channels
	f := media.NewAudioFrame(format, layout, sampleRate, samples)
	plane := f.Planes()[0]

	switch format {
	case media.SampleFormatF32:
		for i, val := range pcm {
			binary.LittleEndian.PutUint32(plane[i*4:(i+1)*4], math.Float32bits(val))
		}
	case media.SampleFormatS16:
		for i, val := range pcm {
			if val > 1.0 {
				val = 1.0
			} else if val < -1.0 {
				val = -1.0
			}
			var s16 int16
			if val < 0 {
				s16 = int16(val * 32768)
			} else {
				s16 = int16(val * 32767)
			}
			binary.LittleEndian.PutUint16(plane[i*2:(i+1)*2], uint16(s16))
		}
	default:
		return nil, fmt.Errorf("unsupported format for creation: %v", format)
	}

	var frame media.Frame = f
	return &frame, nil
}

func convertToFloat32(af *media.AudioFrame) ([]float32, error) {
	plane := af.Planes()[0]
	channels := af.Layout.ChannelCount()
	samples := af.Samples
	totalSamples := samples * channels

	pcm := make([]float32, totalSamples)
	switch af.Format {
	case media.SampleFormatU8:
		for i := 0; i < totalSamples; i++ {
			pcm[i] = (float32(plane[i]) - 128.0) / 128.0
		}
	case media.SampleFormatS16:
		for i := 0; i < totalSamples; i++ {
			val := int16(binary.LittleEndian.Uint16(plane[i*2 : (i+1)*2]))
			pcm[i] = float32(val) / 32768.0
		}
	case media.SampleFormatF32:
		for i := 0; i < totalSamples; i++ {
			bits := binary.LittleEndian.Uint32(plane[i*4 : (i+1)*4])
			pcm[i] = math.Float32frombits(bits)
		}
	default:
		return nil, fmt.Errorf("unsupported sample format in conversion: %v", af.Format)
	}
	return pcm, nil
}

func encodePCMToWAV(pcm []float32, codecID media.CodecID, sampleRate int, layout media.ChannelLayout, format media.SampleFormat) ([]byte, error) {
	frame, err := createAudioFrame(pcm, format, layout, sampleRate)
	if err != nil {
		return nil, err
	}

	encoder := pcmCodec.NewEncoderEngine(pcmCodec.EncoderConfig{
		CodecID:   codecID,
		ByteOrder: binary.LittleEndian,
	})

	buf := testutil.NewBuffer(nil)
	muxer := wavFormat.NewMuxerEngine(buf)

	stream := media.StreamInfo{
		Type: media.MediaAudio,
		MediaAttributes: media.MediaAttributes{
			Codec: codecID,
			Audio: media.AudioAttributes{
				SampleRate:    sampleRate,
				Format:        format,
				ChannelLayout: layout,
			},
		},
	}
	if _, err := muxer.AddStream(stream); err != nil {
		return nil, err
	}
	if err := muxer.WriteHeader(); err != nil {
		return nil, err
	}

	if err := encoder.SendFrame(frame); err != nil {
		return nil, err
	}

	for {
		pkt, err := encoder.ReceivePacket()
		if err == engine.ErrEAGAIN {
			break
		}
		if err != nil {
			return nil, err
		}
		pkt.MediaType = media.MediaAudio
		if err := muxer.WritePacket(0, pkt); err != nil {
			return nil, err
		}
	}

	if err := encoder.Flush(); err != nil {
		return nil, err
	}

	for {
		pkt, err := encoder.ReceivePacket()
		if err == engine.ErrEOF || err == engine.ErrEAGAIN {
			break
		}
		if err != nil {
			return nil, err
		}
		pkt.MediaType = media.MediaAudio
		if err := muxer.WritePacket(0, pkt); err != nil {
			return nil, err
		}
	}

	if err := muxer.WriteTrailer(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func decodeWAVToPCM(wavBytes []byte) ([]float32, error) {
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
	a := stream.MediaAttributes.Audio

	targetFormat := a.Format
	if stream.MediaAttributes.Codec != media.CodecLPCM {
		targetFormat = media.SampleFormatS16
	}

	cfg := pcmCodec.NewConfigWithAudio(a.SampleRate, targetFormat, a.ChannelLayout)
	cfg.CodecID = stream.MediaAttributes.Codec
	decoder := pcmCodec.NewDecoderEngine(cfg)

	var pcmResult []float32

	for {
		packet, _, err := demuxer.ReadPacket()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		if err := decoder.SendPacket(packet); err != nil {
			if err != engine.ErrEAGAIN {
				return nil, err
			}
		}

		for {
			frame, err := decoder.ReceiveFrame()
			if err == engine.ErrEAGAIN {
				break
			}
			if err != nil {
				return nil, err
			}

			audioFrame, ok := (*frame).(*media.AudioFrame)
			if !ok {
				return nil, fmt.Errorf("expected AudioFrame")
			}

			samples, err := convertToFloat32(audioFrame)
			if err != nil {
				return nil, err
			}
			pcmResult = append(pcmResult, samples...)
		}
	}

	if err := decoder.Flush(); err != nil {
		return nil, err
	}

	for {
		frame, err := decoder.ReceiveFrame()
		if err == engine.ErrEOF || err == engine.ErrEAGAIN {
			break
		}
		if err != nil {
			return nil, err
		}

		audioFrame, ok := (*frame).(*media.AudioFrame)
		if !ok {
			return nil, fmt.Errorf("expected AudioFrame")
		}

		samples, err := convertToFloat32(audioFrame)
		if err != nil {
			return nil, err
		}
		pcmResult = append(pcmResult, samples...)
	}

	return pcmResult, nil
}
