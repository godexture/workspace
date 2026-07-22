package internal

import (
	"encoding/binary"
	"fmt"

	imaadpcm "github.com/godexture/codec-pcm/internal/adpcm/ima"
	msadpcm "github.com/godexture/codec-pcm/internal/adpcm/ms"
	"github.com/godexture/codec-pcm/internal/g711"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/format-wav/params"
	"github.com/godexture/sdk/engine"
)

type DecoderConfig struct {
	ByteOrder binary.ByteOrder

	codecID       media.CodecID
	sampleRate    int
	format        media.SampleFormat
	channelLayout media.ChannelLayout
	adpcm         params.ADPCM
}

var DefaultDecoderConfig = DecoderConfig{
	ByteOrder: binary.LittleEndian,

	codecID:       media.CodecLPCM,
	sampleRate:    44100,
	format:        media.SampleFormatS16,
	channelLayout: media.LayoutStereo2_0,
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
	config DecoderConfig
	slot   engine.Slot[*media.Packet]
}

func NewDecoder(stream media.StreamInfo, cfg DecoderConfig) (*Decoder, error) {
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
	if !supportsCodec(cfg.codecID) {
		return nil, fmt.Errorf("codec-pcm decoder does not support codec %q", cfg.codecID)
	}

	return &Decoder{
		config: cfg,
	}, nil
}

func supportsCodec(codec media.CodecID) bool {
	switch codec {
	case media.CodecLPCM, media.CodecPCMU, media.CodecPCMA, media.CodecMSADPCM, media.CodecIMAADPCM:
		return true
	default:
		return false
	}
}

func (d *Decoder) SendPacket(pkt *media.Packet) error {
	if pkt == nil {
		return fmt.Errorf("codec-pcm decoder received nil packet")
	}
	return d.slot.Push(pkt)
}

func (d *Decoder) ReceiveFrame() (*media.Frame, error) {
	pkt, err := d.slot.Receive()
	if err != nil {
		return nil, err
	}

	data := pkt.Data()
	switch d.config.codecID {
	case media.CodecPCMU:
		data = g711.DecodePCMU(data, d.config.ByteOrder)
	case media.CodecPCMA:
		data = g711.DecodePCMA(data, d.config.ByteOrder)
	case media.CodecMSADPCM:
		params, resolveErr := d.resolveADPCMParameters()
		if resolveErr != nil {
			return nil, resolveErr
		}
		data, err = msadpcm.Decode(data, d.config.channelLayout.ChannelCount(), params, d.config.ByteOrder)
		if err != nil {
			return nil, err
		}
	case media.CodecIMAADPCM:
		params, resolveErr := d.resolveADPCMParameters()
		if resolveErr != nil {
			return nil, resolveErr
		}
		data, err = imaadpcm.Decode(data, d.config.channelLayout.ChannelCount(), params, d.config.ByteOrder)
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
	d.slot.Flush()
	return nil
}

func (d *Decoder) Close() error {
	d.slot.Close()
	return nil
}
