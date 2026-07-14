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

type DecoderConfig struct {
	CodecID       media.CodecID
	SampleRate    int
	Format        media.SampleFormat
	ChannelLayout media.ChannelLayout
	ByteOrder     binary.ByteOrder
}

var DefaultDecoderConfig = DecoderConfig{
	CodecID:       media.CodecLPCM,
	SampleRate:    48000,
	Format:        media.SampleFormatS16,
	ChannelLayout: media.LayoutStereo2_0,
	ByteOrder:     binary.LittleEndian,
}

func GetDecodedAttributes(codec media.CodecID, attrs media.AudioAttributes) media.AudioAttributes {
	switch codec {
	case media.CodecPCMU, media.CodecPCMA:
		attrs.Format = media.SampleFormatS16
	case media.CodecMSADPCM, media.CodecIMAADPCM:
		attrs.Format = media.SampleFormatS16
	}

	return attrs
}

type Decoder struct {
	config  DecoderConfig
	pending *media.Packet
	flushed bool
}

func NewDecoder(cfg DecoderConfig) *Decoder {
	isG711 := cfg.CodecID == media.CodecPCMU || cfg.CodecID == media.CodecPCMA
	isADPCM := cfg.CodecID == media.CodecMSADPCM || cfg.CodecID == media.CodecIMAADPCM

	if cfg.SampleRate == 0 && isG711 {
		cfg.SampleRate = 8000
	}
	if cfg.Format == media.SampleFormatUnknown && (isG711 || isADPCM) {
		cfg.Format = media.SampleFormatS16
	}
	if cfg.ChannelLayout.ChannelCount() == 0 && isG711 {
		cfg.ChannelLayout = media.LayoutMono1
	}

	return &Decoder{
		config: cfg,
	}
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
		data, err = msadpcm.Decode(data, d.config.ChannelLayout.ChannelCount(), d.config.ByteOrder)
		if err != nil {
			return nil, err
		}
	case media.CodecIMAADPCM:
		data, err = imaadpcm.Decode(data, d.config.ChannelLayout.ChannelCount(), d.config.ByteOrder)
		if err != nil {
			return nil, err
		}
	}

	outAttrs := GetDecodedAttributes(d.config.CodecID, media.AudioAttributes{
		SampleRate:    d.config.SampleRate,
		ChannelLayout: d.config.ChannelLayout,
		Format:        d.config.Format,
	})

	bytesPerSample := outAttrs.Format.BytesPerSample()
	channels := outAttrs.ChannelLayout.ChannelCount()
	samples := len(data) / (bytesPerSample * channels)

	f := media.NewAudioFrame(
		outAttrs.Format,
		outAttrs.ChannelLayout,
		outAttrs.SampleRate,
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
