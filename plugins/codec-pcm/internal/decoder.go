package internal

import (
	"errors"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/engine"
)

type Config struct {
	CodecID    media.CodecID
	SampleRate int
	Format     media.SampleFormat
	Layout     media.ChannelLayout
}

func (Config) NodeConfigaration() {}

func DefaultConfig() Config {
	return Config{
		CodecID:    media.CodecLPCM,
		SampleRate: 48000,
		Format:     media.SampleFormatS16,
		Layout:     media.LayoutStereo2_0,
	}
}

type Decoder struct {
	config  Config
	pending *media.Packet
	flushed bool
}

func NewDecoder(config Config) *Decoder {
	if config.CodecID == "" {
		config.CodecID = media.CodecLPCM
	}
	if config.SampleRate <= 0 {
		config.SampleRate = 48000
	}
	if config.Format == media.SampleFormatUnknown {
		config.Format = media.SampleFormatS16
	}
	if config.Layout.ChannelCount() <= 0 {
		config.Layout = media.LayoutStereo2_0
	}

	return &Decoder{config: config}
}

func (d *Decoder) SendPacket(pkt *media.Packet) error {
	if pkt == nil {
		return errors.New("codec-pcm decoder received nil packet")
	}
	if d.pending != nil {
		return errors.New("codec-pcm decoder has an unconsumed packet")
	}

	d.pending = pkt
	return nil
}

func (d *Decoder) ReceiveFrame() (*media.Frame, error) {
	if d.pending == nil {
		if d.flushed {
			return nil, engine.ErrEOF
		}
		return nil, engine.ErrEAGAIN
	}

	pkt := d.pending
	d.pending = nil

	data := pkt.Data()
	switch d.config.CodecID {
	case media.CodecPCMU:
		data = DecodePCMU(data)
	case media.CodecPCMA:
		data = DecodePCMA(data)
	}

	bytesPerSample := d.config.Format.BytesPerSample()
	channels := d.config.Layout.ChannelCount()
	samples := len(data) / (bytesPerSample * channels)

	f := media.NewAudioFrame(
		d.config.Format,
		d.config.Layout,
		d.config.SampleRate,
		samples,
		media.WithAudioPts(pkt.PTS),
	)
	copy(f.Planes()[0], data)

	var frame media.Frame = f
	return &frame, nil
}

func (d *Decoder) Flush() error {
	d.flushed = true
	return nil
}
