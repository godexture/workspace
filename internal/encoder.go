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

type EncoderConfig struct {
	CodecID   media.CodecID
	ByteOrder binary.ByteOrder
}

func (EncoderConfig) NodeConfiguration() {}

type Encoder struct {
	config  EncoderConfig
	pending *media.Packet
	flushed bool
}

func NewEncoder(config EncoderConfig) *Encoder {
	if config.CodecID == "" {
		config.CodecID = media.CodecLPCM
	}
	if config.ByteOrder == nil {
		config.ByteOrder = binary.LittleEndian
	}
	return &Encoder{config: config}
}

func (e *Encoder) SendFrame(frame *media.Frame) error {
	if frame == nil || *frame == nil {
		return errors.New("codec-pcm encoder received nil frame")
	}
	if e.pending != nil {
		return errors.New("codec-pcm encoder has an unconsumed packet")
	}

	f := *frame
	af, ok := f.(*media.AudioFrame)
	if !ok {
		return errors.New("codec-pcm encoder expected *media.AudioFrame")
	}

	data := af.Planes()[0]
	var err error
	switch e.config.CodecID {
	case media.CodecPCMU:
		data = g711.EncodePCMU(data, e.config.ByteOrder)
	case media.CodecPCMA:
		data = g711.EncodePCMA(data, e.config.ByteOrder)
	case media.CodecMSADPCM:
		data, err = msadpcm.Encode(data, af.Layout.ChannelCount(), e.config.ByteOrder)
		if err != nil {
			return err
		}
	case media.CodecIMAADPCM:
		data, err = imaadpcm.Encode(data, af.Layout.ChannelCount(), e.config.ByteOrder)
		if err != nil {
			return err
		}
	}

	pkt := media.NewPacket(len(data))
	copy(pkt.Data(), data)
	pkt.MediaType = media.MediaAudio
	pkt.StreamIndex = 0
	pkt.PTS = f.Pts()
	pkt.DTS = media.Dts(f.Pts())

	e.pending = pkt
	return nil
}

func (e *Encoder) ReceivePacket() (*media.Packet, error) {
	if e.pending == nil {
		if e.flushed {
			return nil, engine.ErrEOF
		}
		return nil, engine.ErrEAGAIN
	}

	pkt := e.pending
	e.pending = nil
	return pkt, nil
}

func (e *Encoder) Flush() error {
	e.flushed = true
	return nil
}
