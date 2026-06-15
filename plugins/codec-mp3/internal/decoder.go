package internal

import (
	"encoding/binary"
	"errors"

	"github.com/godexture/codec-mp3/internal/mp3"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/engine"
)

// DecoderConfig はMP3デコーダの設定。
type DecoderConfig struct{}

func (DecoderConfig) NodeConfigaration() {}

type Decoder struct {
	packets    []*media.Packet
	dec        mp3.Mp3Dec
	floatPcm   []float32
	intPcm     []int16
	sampleRate int
	channels   int
	flushed    bool
	err        error
}

func NewDecoder() *Decoder {
	d := &Decoder{
		floatPcm:  make([]float32, 1152*2),
		intPcm:    make([]int16, 1152*2),
	}
	d.dec.Init()
	return d
}

// processFrame processes raw PCM float samples, converts them to S16 format,
// and packages them into a media.Frame.
func processFrame(floatPcm []float32, intPcm []int16, samples int, info mp3.Mp3DecFrameInfo) (media.Frame, error) {
	channels := info.Channels
	decodedSamples := samples * channels
	mp3.FloatToS16(floatPcm[:decodedSamples], intPcm[:decodedSamples])

	var layout media.ChannelLayout
	if channels == 1 {
		layout = media.LayoutMono1
	} else {
		layout = media.LayoutStereo2_0
	}

	frame := media.NewAudioFrame(
		media.SampleFormatS16,
		layout,
		info.Hz,
		samples,
	)

	plane := frame.Planes()[0]
	for i := 0; i < decodedSamples; i++ {
		binary.LittleEndian.PutUint16(plane[i*2:], uint16(intPcm[i]))
	}
	return frame, nil
}

func (d *Decoder) SendPacket(pkt *media.Packet) error {
	if pkt == nil {
		return errors.New("codec-mp3 decoder: received nil packet")
	}
	if d.flushed {
		return engine.ErrEOF
	}
	if d.err != nil {
		return d.err
	}

	d.packets = append(d.packets, pkt)
	return nil
}

func (d *Decoder) ReceiveFrame() (*media.Frame, error) {
	if d.err != nil {
		return nil, d.err
	}

	if len(d.packets) == 0 {
		if d.flushed {
			return nil, engine.ErrEOF
		}
		return nil, engine.ErrEAGAIN
	}

	// Pop the first packet
	pkt := d.packets[0]
	d.packets = d.packets[1:]

	samples, info := d.dec.DecodeFrame(pkt.Data(), d.floatPcm)
	if info.FrameBytes > 0 {
		if samples > 0 {
			if d.sampleRate == 0 {
				d.sampleRate = info.Hz
				d.channels = info.Channels
			} else if info.Hz != d.sampleRate || info.Channels != d.channels {
				d.err = errors.New("codec-mp3 decoder: sample rate or channels changed mid-stream")
				return nil, d.err
			}

			frame, err := processFrame(d.floatPcm, d.intPcm, samples, info)
			if err != nil {
				d.err = err
				return nil, d.err
			}
			return &frame, nil
		}
		return nil, engine.ErrEAGAIN
	}

	return nil, engine.ErrEAGAIN
}

func (d *Decoder) Flush() error {
	d.flushed = true
	return nil
}
