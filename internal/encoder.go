package internal

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	imaadpcm "github.com/godexture/codec-pcm/internal/adpcm/ima"
	msadpcm "github.com/godexture/codec-pcm/internal/adpcm/ms"
	"github.com/godexture/codec-pcm/internal/g711"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/buffer"
	"github.com/godexture/sdk/engine"
)

type EncoderConfig struct {
	CodecID   media.CodecID
	ByteOrder binary.ByteOrder
}

func (EncoderConfig) NodeConfiguration() {}

type Encoder struct {
	config       EncoderConfig
	pendingQueue []*media.Packet
	flushed      bool
	buf          *buffer.BlockBuffer
	lastChannels int
	lastPts      media.Pts

	imaState *imaadpcm.EncodeState
}

func NewEncoder(config EncoderConfig) *Encoder {
	if config.CodecID == "" {
		config.CodecID = media.CodecLPCM
	}
	if config.ByteOrder == nil {
		config.ByteOrder = binary.LittleEndian
	}
	return &Encoder{
		config:   config,
		buf:      &buffer.BlockBuffer{},
		imaState: &imaadpcm.EncodeState{},
	}
}

func (e *Encoder) SendFrame(frame *media.Frame) error {
	if frame == nil || *frame == nil {
		return errors.New("codec-pcm encoder received nil frame")
	}

	f := *frame
	af, ok := f.(*media.AudioFrame)
	if !ok {
		return errors.New("codec-pcm encoder expected *media.AudioFrame")
	}

	data := af.Planes()[0]
	if e.config.CodecID != media.CodecLPCM {
		if af.Format == media.SampleFormatF32 {
			data = convertF32ToS16(data)
		} else if af.Format != media.SampleFormatS16 {
			return fmt.Errorf("unsupported sample format for pcm encoder: %v", af.Format)
		}
	}

	e.lastChannels = af.Layout.ChannelCount()
	e.lastPts = f.Pts()

	var err error
	switch e.config.CodecID {
	case media.CodecPCMU:
		data = g711.EncodePCMU(data, e.config.ByteOrder)
	case media.CodecPCMA:
		data = g711.EncodePCMA(data, e.config.ByteOrder)
	case media.CodecMSADPCM:
		bytesPerBlock := msadpcm.BytesPerPCMBlock(e.lastChannels)
		e.buf.Append(data)
		toEncode := e.buf.TakeBlocks(bytesPerBlock)
		if toEncode == nil {
			return nil
		}
		data, err = msadpcm.Encode(toEncode, e.lastChannels, e.config.ByteOrder)
		if err != nil {
			return err
		}
	case media.CodecIMAADPCM:
		bytesPerBlock := imaadpcm.BytesPerPCMBlock(e.lastChannels)
		e.buf.Append(data)
		toEncode := e.buf.TakeBlocks(bytesPerBlock)
		if toEncode == nil {
			return nil
		}
		data, err = imaadpcm.Encode(toEncode, e.lastChannels, e.config.ByteOrder, e.imaState)
		if err != nil {
			return err
		}
	}

	pkt := media.NewPacket(len(data))
	copy(pkt.Data(), data)
	pkt.MediaType = media.MediaAudio
	pkt.StreamIndex = 0
	pkt.PTS = e.lastPts
	pkt.DTS = media.Dts(e.lastPts)

	e.pendingQueue = append(e.pendingQueue, pkt)
	return nil
}

func (e *Encoder) ReceivePacket() (*media.Packet, error) {
	if len(e.pendingQueue) == 0 {
		if e.flushed {
			return nil, engine.ErrEOF
		}
		return nil, engine.ErrEAGAIN
	}

	pkt := e.pendingQueue[0]
	e.pendingQueue = e.pendingQueue[1:]
	return pkt, nil
}

func (e *Encoder) Flush() error {
	e.flushed = true
	remains := e.buf.TakeAll()
	if len(remains) > 0 {
		var data []byte
		var err error
		switch e.config.CodecID {
		case media.CodecMSADPCM:
			data, err = msadpcm.Encode(remains, e.lastChannels, e.config.ByteOrder)
		case media.CodecIMAADPCM:
			data, err = imaadpcm.Encode(remains, e.lastChannels, e.config.ByteOrder, e.imaState)
		}
		if err != nil {
			return err
		}

		if len(data) > 0 {
			pkt := media.NewPacket(len(data))
			copy(pkt.Data(), data)
			pkt.MediaType = media.MediaAudio
			pkt.StreamIndex = 0
			pkt.PTS = e.lastPts
			pkt.DTS = media.Dts(e.lastPts)
			e.pendingQueue = append(e.pendingQueue, pkt)
		}
	}
	return nil
}

func convertF32ToS16(f32Data []byte) []byte {
	samples := len(f32Data) / 4
	s16Data := make([]byte, samples*2)
	for i := 0; i < samples; i++ {
		fBits := binary.LittleEndian.Uint32(f32Data[i*4 : i*4+4])
		fVal := math.Float32frombits(fBits)

		if fVal > 1.0 {
			fVal = 1.0
		} else if fVal < -1.0 {
			fVal = -1.0
		}

		var s16Val int16
		if fVal < 0 {
			s16Val = int16(fVal * 32768)
		} else {
			s16Val = int16(fVal * 32767)
		}
		binary.LittleEndian.PutUint16(s16Data[i*2:i*2+2], uint16(s16Val))
	}
	return s16Data
}
