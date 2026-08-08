package internal

import (
	"encoding/binary"
	"math"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/plugin/mp3/internal/codec/mp3"
	"github.com/godexture/godec/plugin/mp3/internal/codec/mp3/domain"
	"github.com/godexture/godec/sdk/engine"
)

type Decoder struct {
	packets           []*media.Packet
	decoder           mp3.Decoder
	float32PCMSamples []float32
	sampleRate        int
	channelCount      int
	isFlushed         bool
	lastErr           error
}

func NewDecoder() *Decoder {
	d := &Decoder{
		float32PCMSamples: make([]float32, mp3.SamplesPerFrameLayer23*mp3.MaxChannels),
	}
	d.decoder.Init()
	return d
}

// processFrame processes raw PCM float samples and packages them into a media.Frame.
func processFrame(float32PCMSamples []float32, sampleCount int, frameInfo domain.FrameInfo, pts media.Pts) (media.Frame, error) {
	channelCount := frameInfo.Channels
	totalDecodedSamples := sampleCount * channelCount

	var channelLayout media.ChannelLayout
	if channelCount == 1 {
		channelLayout = media.LayoutMono1
	} else {
		channelLayout = media.LayoutStereo2_0
	}

	audioFrame := media.NewAudioFrame(
		media.SampleFormatF32,
		channelLayout,
		frameInfo.SampleRateHertz,
		sampleCount,
		media.WithAudioPts(pts),
	)

	audioPlane := audioFrame.Planes()[0]
	for i := 0; i < totalDecodedSamples; i++ {
		binary.LittleEndian.PutUint32(audioPlane[i*4:], math.Float32bits(float32PCMSamples[i]))
	}
	return audioFrame, nil
}

func (d *Decoder) SendPacket(packet *media.Packet) error {
	if packet == nil {
		return domain.ErrNilPacket
	}
	if d.isFlushed {
		return engine.ErrEOF
	}
	if d.lastErr != nil {
		return d.lastErr
	}

	d.packets = append(d.packets, packet)
	return nil
}

func (d *Decoder) ReceiveFrame() (media.Frame, error) {
	if d.lastErr != nil {
		return nil, d.lastErr
	}

	if len(d.packets) == 0 {
		if d.isFlushed {
			return nil, engine.ErrEOF
		}
		return nil, engine.ErrEAGAIN
	}

	// Pop the first packet
	packet := d.packets[0]
	d.packets = d.packets[1:]

	sampleCount, frameInfo, err := d.decoder.DecodeFrame(packet.Data(), d.float32PCMSamples)
	if err != nil {
		// Frame-level decoding errors (like reservoir underflow or corrupted frames)
		// are transient. Return ErrEAGAIN so the pipeline can process subsequent packets.
		return nil, engine.ErrEAGAIN
	}
	if frameInfo.FrameBytes > 0 {
		if sampleCount > 0 {
			if d.sampleRate == 0 {
				d.sampleRate = frameInfo.SampleRateHertz
				d.channelCount = frameInfo.Channels
			} else if frameInfo.SampleRateHertz != d.sampleRate || frameInfo.Channels != d.channelCount {
				d.lastErr = domain.ErrFormatChanged
				return nil, d.lastErr
			}

			audioFrame, err := processFrame(d.float32PCMSamples, sampleCount, frameInfo, packet.PTS)
			if err != nil {
				d.lastErr = err
				return nil, d.lastErr
			}
			return audioFrame, nil
		}
		return nil, engine.ErrEAGAIN
	}

	return nil, engine.ErrEAGAIN
}

func (d *Decoder) Flush() error {
	d.isFlushed = true
	return nil
}
