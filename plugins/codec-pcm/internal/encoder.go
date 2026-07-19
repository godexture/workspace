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
	"github.com/godexture/format-wav/params"
	"github.com/godexture/sdk/buffer"
	"github.com/godexture/sdk/dsp"
	"github.com/godexture/sdk/engine"
)

type EncoderConfig struct {
	CodecID media.CodecID
	ADPCM   params.ADPCM

	byteOrder binary.ByteOrder
}

var DefaultEncoderConfig = EncoderConfig{
	CodecID:   media.CodecLPCM,
	byteOrder: binary.LittleEndian,
}

type Encoder struct {
	config       EncoderConfig
	pendingQueue []*media.Packet
	flushed      bool
	buf          buffer.Ring[byte]
	lastChannels int
	lastPts      media.Pts

	imaState *imaadpcm.EncodeState
	adpcm    params.ADPCM
}

func adpcmParametersFromStream(stream media.StreamInfo, target media.CodecID) (params.ADPCM, bool, error) {
	if media.IsCodecParameters[params.ADPCM](stream.CodecParameters) {
		adpcm, err := params.Parse(target, stream.Audio.ChannelLayout.ChannelCount(), stream.CodecParameters.Data)
		if err == nil {
			return adpcm, true, nil
		}
		return params.ADPCM{}, false, err
	}
	return params.ADPCM{}, false, nil
}

func NewEncoder(stream media.StreamInfo, target media.CodecID, cfg EncoderConfig) (*Encoder, error) {
	if cfg.CodecID == media.CodecLPCM && target != media.CodecLPCM {
		cfg.CodecID = target
	} else if cfg.CodecID != target && cfg.CodecID != media.CodecLPCM {
		cfg.CodecID = target
	}

	if stream.Codec == target {
		if adpcm, ok, _ := adpcmParametersFromStream(stream, target); ok {
			if cfg.ADPCM.BlockAlign == 0 {
				cfg.ADPCM = adpcm
			}
		}
	}

	if (target == media.CodecMSADPCM || target == media.CodecIMAADPCM) && cfg.ADPCM.BlockAlign == 0 {
		channels := stream.Audio.ChannelLayout.ChannelCount()
		if channels == 0 {
			channels = 1
		}

		p, _ := params.Default(target, channels)
		cfg.ADPCM = p
	}

	return &Encoder{
		config:   cfg,
		adpcm:    cfg.ADPCM,
		imaState: &imaadpcm.EncodeState{},
	}, nil
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

	data := leftJustifyPCM(af.Planes()[0], af.Format, af.BitsPerSample)
	if e.config.CodecID != media.CodecLPCM {
		if af.Format == media.SampleFormatF32 {
			data = convertF32ToS16(data)
		} else if af.Format != media.SampleFormatS16 {
			return fmt.Errorf("unsupported sample format for pcm encoder: %v", af.Format)
		}
	}

	e.lastChannels = af.Layout.ChannelCount()
	e.lastPts = f.Pts()
	if e.config.CodecID == media.CodecMSADPCM || e.config.CodecID == media.CodecIMAADPCM {
		params, err := e.resolveADPCMParameters(e.lastChannels)
		if err != nil {
			return err
		}
		e.adpcm = params
	}

	var err error
	switch e.config.CodecID {
	case media.CodecPCMU:
		data = g711.EncodePCMU(data, e.config.byteOrder)
	case media.CodecPCMA:
		data = g711.EncodePCMA(data, e.config.byteOrder)
	case media.CodecMSADPCM:
		bytesPerBlock := msadpcm.BytesPerPCMBlock(e.lastChannels, int(e.adpcm.BlockAlign))
		e.buf.Append(data)
		toEncode := e.takeBlocks(bytesPerBlock)
		if toEncode == nil {
			return nil
		}
		data, err = msadpcm.Encode(toEncode, e.lastChannels, e.adpcm, e.config.byteOrder)
		if err != nil {
			return err
		}
		return e.enqueueADPCMPackets(data)
	case media.CodecIMAADPCM:
		bytesPerBlock := imaadpcm.BytesPerPCMBlock(e.lastChannels, int(e.adpcm.BlockAlign))
		e.buf.Append(data)
		toEncode := e.takeBlocks(bytesPerBlock)
		if toEncode == nil {
			return nil
		}
		data, err = imaadpcm.Encode(toEncode, e.lastChannels, e.adpcm, e.config.byteOrder, e.imaState)
		if err != nil {
			return err
		}
		return e.enqueueADPCMPackets(data)
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
			data, err = msadpcm.Encode(remains, e.lastChannels, e.adpcm, e.config.byteOrder)
		case media.CodecIMAADPCM:
			data, err = imaadpcm.Encode(remains, e.lastChannels, e.adpcm, e.config.byteOrder, e.imaState)
		}
		if err != nil {
			return err
		}

		if len(data) > 0 {
			return e.enqueueADPCMPackets(data)
		}
	}
	return nil
}

func (e *Encoder) takeBlocks(bytesPerBlock int) []byte {
	if bytesPerBlock <= 0 {
		return nil
	}
	n := e.buf.Len() / bytesPerBlock * bytesPerBlock
	return e.buf.Take(n)
}

func (e *Encoder) enqueueADPCMPackets(data []byte) error {
	blockAlign := int(e.adpcm.BlockAlign)
	if blockAlign == 0 || len(data)%blockAlign != 0 {
		return fmt.Errorf("invalid ADPCM encoded data size %d for block align %d", len(data), blockAlign)
	}
	for offset := 0; offset < len(data); offset += blockAlign {
		pkt := media.NewPacket(blockAlign)
		copy(pkt.Data(), data[offset:offset+blockAlign])
		pkt.MediaType = media.MediaAudio
		pkt.StreamIndex = 0
		pkt.PTS = e.lastPts
		pkt.DTS = media.Dts(e.lastPts)
		e.pendingQueue = append(e.pendingQueue, pkt)
	}
	return nil
}

func (e *Encoder) resolveADPCMParameters(channels int) (params.ADPCM, error) {
	if e.config.ADPCM.BlockAlign != 0 {
		if err := e.config.ADPCM.Validate(e.config.CodecID, channels); err != nil {
			return params.ADPCM{}, err
		}
		return e.config.ADPCM, nil
	}
	return params.Default(e.config.CodecID, channels)
}

// leftJustifyPCM shifts samples that occupy only the low BitsPerSample bits of
// their container format (e.g. 24-bit FLAC output carried in S24/S32 frames)
// up to full scale, which is what raw LPCM byte streams are expected to hold.
func leftJustifyPCM(data []byte, format media.SampleFormat, bitsPerSample int) []byte {
	containerBits := format.BytesPerSample() * 8
	if bitsPerSample <= 0 || bitsPerSample >= containerBits {
		return data
	}
	shift := uint(containerBits - bitsPerSample)
	out := make([]byte, len(data))
	switch format {
	case media.SampleFormatS16:
		leftJustifyS16(out, data, shift)
	case media.SampleFormatS24:
		for i := 0; i+3 <= len(data); i += 3 {
			v := (uint32(data[i]) | uint32(data[i+1])<<8 | uint32(data[i+2])<<16) << shift
			out[i] = byte(v)
			out[i+1] = byte(v >> 8)
			out[i+2] = byte(v >> 16)
		}
	case media.SampleFormatS32:
		leftJustifyS32(out, data, shift)
	default:
		return data
	}
	return out
}

func convertF32ToS16(f32Data []byte) []byte {
	samples := len(f32Data) / 4
	s16Data := make([]byte, samples*2)
	source := dsp.AsSamples[float32](f32Data)
	destination := dsp.AsSamples[int16](s16Data)
	if source != nil && destination != nil {
		dsp.ConvertF32ToS16(destination, source)
		return s16Data
	}
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
