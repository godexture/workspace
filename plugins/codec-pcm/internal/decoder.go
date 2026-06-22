package internal

import (
	"encoding/binary"
	"errors"

	imaadpcm "github.com/godexture/codec-pcm/internal/adpcm/ima"
	msadpcm "github.com/godexture/codec-pcm/internal/adpcm/ms"
	"github.com/godexture/codec-pcm/internal/g711"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/engine"
)

type Config struct {
	CodecID    media.CodecID
	SampleRate int
	Format     media.SampleFormat
	Layout     media.ChannelLayout
	ByteOrder  binary.ByteOrder
}

func (Config) NodeConfiguration() {}

func DefaultConfig() Config {
	return Config{
		CodecID:    media.CodecLPCM,
		SampleRate: 48000,
		Format:     media.SampleFormatS16,
		Layout:     media.LayoutStereo2_0,
		ByteOrder:  binary.LittleEndian,
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
	if config.ByteOrder == nil {
		config.ByteOrder = binary.LittleEndian
	}

	isG711 := config.CodecID == media.CodecPCMU || config.CodecID == media.CodecPCMA
	isADPCM := config.CodecID == media.CodecMSADPCM || config.CodecID == media.CodecIMAADPCM

	if config.SampleRate <= 0 {
		if isG711 {
			config.SampleRate = 8000
		} else {
			config.SampleRate = 48000
		}
	}
	if config.Format == media.SampleFormatUnknown {
		if isG711 || isADPCM {
			config.Format = media.SampleFormatS16
		} else {
			config.Format = media.SampleFormatS16
		}
	}
	if config.Layout.ChannelCount() <= 0 {
		if isG711 {
			config.Layout = media.LayoutMono1
		} else {
			config.Layout = media.LayoutStereo2_0
		}
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
	var err error
	switch d.config.CodecID {
	case media.CodecPCMU:
		data = g711.DecodePCMU(data, d.config.ByteOrder)
	case media.CodecPCMA:
		data = g711.DecodePCMA(data, d.config.ByteOrder)
	case media.CodecMSADPCM:
		data, err = msadpcm.Decode(data, d.config.Layout.ChannelCount(), d.config.ByteOrder)
		if err != nil {
			return nil, err
		}
	case media.CodecIMAADPCM:
		data, err = imaadpcm.Decode(data, d.config.Layout.ChannelCount(), d.config.ByteOrder)
		if err != nil {
			return nil, err
		}
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
