package internal

import (
	"encoding/binary"
	"errors"

	imaadpcm "github.com/godexture/codec-pcm/internal/adpcm/ima"
	msadpcm "github.com/godexture/codec-pcm/internal/adpcm/ms"
	"github.com/godexture/codec-pcm/internal/g711"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/format-wav/params"
	"github.com/godexture/sdk/engine"
)

type DecoderConfig struct {
	codecID       media.CodecID
	sampleRate    int
	format        media.SampleFormat
	channelLayout media.ChannelLayout
	byteOrder     binary.ByteOrder
	adpcm         params.ADPCM
}

var DefaultDecoderConfig = DecoderConfig{
	codecID:       media.CodecLPCM,
	sampleRate:    48000,
	format:        media.SampleFormatS16,
	channelLayout: media.LayoutStereo2_0,
	byteOrder:     binary.LittleEndian,
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

func NewDecoder(stream media.StreamInfo, cfg DecoderConfig) *Decoder {
	if stream.MediaAttributes.Codec != "" {
		cfg.codecID = stream.MediaAttributes.Codec
	}
	if stream.MediaAttributes.Audio.SampleRate > 0 {
		cfg.sampleRate = stream.MediaAttributes.Audio.SampleRate
	}
	if stream.MediaAttributes.Audio.Format != media.SampleFormatUnknown {
		cfg.format = stream.MediaAttributes.Audio.Format
	}
	if stream.MediaAttributes.Audio.ChannelLayout.ChannelCount() > 0 {
		cfg.channelLayout = stream.MediaAttributes.Audio.ChannelLayout
	}
	if media.IsCodecParameters[params.ADPCM](stream.CodecParameters) {
		if adpcm, err := params.Parse(stream.Codec, stream.Audio.ChannelLayout.ChannelCount(), stream.CodecParameters.Data); err == nil {
			cfg.adpcm = adpcm
		}
	}

	isG711 := cfg.codecID == media.CodecPCMU || cfg.codecID == media.CodecPCMA
	isADPCM := cfg.codecID == media.CodecMSADPCM || cfg.codecID == media.CodecIMAADPCM

	if cfg.sampleRate == 0 && isG711 {
		cfg.sampleRate = 8000
	}
	if cfg.format == media.SampleFormatUnknown && (isG711 || isADPCM) {
		cfg.format = media.SampleFormatS16
	}
	if cfg.channelLayout.ChannelCount() == 0 && isG711 {
		cfg.channelLayout = media.LayoutMono1
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
	switch d.config.codecID {
	case media.CodecPCMU:
		data = g711.DecodePCMU(data, d.config.byteOrder)
	case media.CodecPCMA:
		data = g711.DecodePCMA(data, d.config.byteOrder)
	case media.CodecMSADPCM:
		params, resolveErr := d.resolveADPCMParameters()
		if resolveErr != nil {
			return nil, resolveErr
		}
		data, err = msadpcm.Decode(data, d.config.channelLayout.ChannelCount(), params, d.config.byteOrder)
		if err != nil {
			return nil, err
		}
	case media.CodecIMAADPCM:
		params, resolveErr := d.resolveADPCMParameters()
		if resolveErr != nil {
			return nil, resolveErr
		}
		data, err = imaadpcm.Decode(data, d.config.channelLayout.ChannelCount(), params, d.config.byteOrder)
		if err != nil {
			return nil, err
		}
	}

	outAttrs := GetDecodedAttributes(d.config.codecID, media.AudioAttributes{
		SampleRate:    d.config.sampleRate,
		ChannelLayout: d.config.channelLayout,
		Format:        d.config.format,
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

func (d *Decoder) resolveADPCMParameters() (params.ADPCM, error) {
	channels := d.config.channelLayout.ChannelCount()
	if d.config.adpcm.BlockAlign != 0 {
		if err := d.config.adpcm.Validate(d.config.codecID, channels); err != nil {
			return params.ADPCM{}, err
		}
		return d.config.adpcm, nil
	}
	return params.Default(d.config.codecID, channels)
}

func (d *Decoder) Flush() error {
	d.flushed = true
	return nil
}
