package internal

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
)

type Muxer struct {
	w io.Writer

	streamSet bool
	stream    media.StreamInfo
	meta      metadata.Bundle
	packets   [][]byte
	closed    bool
}

func NewMuxer(w io.Writer) *Muxer {
	return &Muxer{w: w}
}

func (m *Muxer) AddStream(info media.StreamInfo) (int, error) {
	if m.streamSet {
		return 0, errors.New("wav muxer supports only one audio stream")
	}

	if info.Type != media.MediaAudio {
		return 0, errors.New("wav muxer expects an audio stream")
	}

	if info.MediaAttributes.Audio.Format == media.SampleFormatUnknown {
		return 0, errors.New("wav muxer requires an audio sample format")
	}

	m.stream = info
	m.streamSet = true
	return 0, nil
}

func (m *Muxer) SetMetadata(meta metadata.Bundle) error {
	m.meta = meta
	return nil
}

func (m *Muxer) WriteHeader() error {
	if !m.streamSet {
		return errors.New("wav muxer stream is not configured")
	}

	return nil
}

func (m *Muxer) WritePacket(streamIndex int, pkt *media.Packet) error {
	if !m.streamSet {
		return errors.New("wav muxer stream is not configured")
	}

	if streamIndex != 0 {
		return fmt.Errorf("wav muxer only accepts stream 0, got %d", streamIndex)
	}

	if pkt == nil {
		return errors.New("wav muxer received nil packet")
	}

	data := append([]byte(nil), pkt.Data()...)
	m.packets = append(m.packets, data)
	return nil
}

func buildWAVHeader(attr media.MediaAttributes, dataSize uint64) ([]byte, error) {
	formatTag, bitsPerSample, err := wavFormatForMediaAttributes(attr)
	if err != nil {
		return nil, err
	}

	layout := attr.Audio.ChannelLayout
	channels := layout.ChannelCount()
	if channels <= 0 {
		return nil, errors.New("wav muxer requires a valid channel layout")
	}

	sampleRate := attr.Audio.SampleRate
	if sampleRate <= 0 {
		return nil, errors.New("wav muxer requires a valid sample rate")
	}

	blockAlign := channels * int(bitsPerSample/8)
	byteRate := sampleRate * blockAlign

	pad := uint64(0)
	if dataSize%2 == 1 {
		pad = 1
	}

	useExtensible := channels >= 3 || bitsPerSample > 16
	if !useExtensible {
		defaultLayout := layoutFromChannelCount(channels)
		if layout.Mask() != defaultLayout.Mask() {
			useExtensible = true
		}
	}

	fmtSize := uint32(16)
	if useExtensible {
		fmtSize = 40
	}

	writeFormatTag := formatTag
	if useExtensible {
		writeFormatTag = wavAudioExtensible
	}

	writeFact := writeFormatTag != wavAudioPCM

	factSize := uint32(0)
	if writeFact {
		factSize = 12
	}

	riffSize := uint64(4) + 8 + uint64(fmtSize) + uint64(factSize) + 8 + dataSize + pad
	useRF64 := riffSize >= 0xFFFFFFFF

	ds64TotalSize := uint64(0)
	if useRF64 {
		ds64TotalSize = 36 // 8 bytes header + 28 bytes payload
		riffSize += ds64TotalSize
	}

	var headerBuf bytes.Buffer

	if useRF64 {
		headerBuf.WriteString("RF64")
		binary.Write(&headerBuf, binary.LittleEndian, uint32(0xFFFFFFFF))
	} else {
		headerBuf.WriteString("RIFF")
		binary.Write(&headerBuf, binary.LittleEndian, uint32(riffSize))
	}

	headerBuf.WriteString("WAVE")

	if useRF64 {
		headerBuf.WriteString("ds64")
		binary.Write(&headerBuf, binary.LittleEndian, uint32(28))
		binary.Write(&headerBuf, binary.LittleEndian, riffSize)
		binary.Write(&headerBuf, binary.LittleEndian, dataSize)
		numSamples := uint64(0)
		if blockAlign > 0 {
			numSamples = dataSize / uint64(blockAlign)
		}
		binary.Write(&headerBuf, binary.LittleEndian, numSamples)
		binary.Write(&headerBuf, binary.LittleEndian, uint32(0)) // tableLength
	}

	headerBuf.WriteString("fmt ")
	binary.Write(&headerBuf, binary.LittleEndian, fmtSize)
	binary.Write(&headerBuf, binary.LittleEndian, writeFormatTag)
	binary.Write(&headerBuf, binary.LittleEndian, uint16(channels))
	binary.Write(&headerBuf, binary.LittleEndian, uint32(sampleRate))
	binary.Write(&headerBuf, binary.LittleEndian, uint32(byteRate))
	binary.Write(&headerBuf, binary.LittleEndian, uint16(blockAlign))
	binary.Write(&headerBuf, binary.LittleEndian, bitsPerSample)

	if useExtensible {
		binary.Write(&headerBuf, binary.LittleEndian, uint16(22)) // cbSize
		binary.Write(&headerBuf, binary.LittleEndian, bitsPerSample) // validBitsPerSample
		binary.Write(&headerBuf, binary.LittleEndian, uint32(layout.Mask())) // channelMask
		
		var subFormatBase = []byte{0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}
		binary.Write(&headerBuf, binary.LittleEndian, formatTag)
		binary.Write(&headerBuf, binary.LittleEndian, uint16(0))
		headerBuf.Write(subFormatBase)
	}

	if writeFact {
		headerBuf.WriteString("fact")
		binary.Write(&headerBuf, binary.LittleEndian, uint32(4))
		numSamples := uint64(0)
		if blockAlign > 0 {
			numSamples = dataSize / uint64(blockAlign)
		}
		if useRF64 && numSamples > 0xFFFFFFFF {
			binary.Write(&headerBuf, binary.LittleEndian, uint32(0xFFFFFFFF))
		} else {
			binary.Write(&headerBuf, binary.LittleEndian, uint32(numSamples))
		}
	}

	headerBuf.WriteString("data")
	if useRF64 {
		binary.Write(&headerBuf, binary.LittleEndian, uint32(0xFFFFFFFF))
	} else {
		binary.Write(&headerBuf, binary.LittleEndian, uint32(dataSize))
	}

	return headerBuf.Bytes(), nil
}

func (m *Muxer) WriteTrailer() error {
	if m.closed {
		return nil
	}
	defer func() {
		m.closed = true
	}()

	if !m.streamSet {
		return errors.New("wav muxer stream is not configured")
	}

	var dataSize uint64
	for _, pkt := range m.packets {
		dataSize += uint64(len(pkt))
	}

	headerBytes, err := buildWAVHeader(m.stream.MediaAttributes, dataSize)
	if err != nil {
		return err
	}

	if _, err := m.w.Write(headerBytes); err != nil {
		return err
	}

	for _, pkt := range m.packets {
		if _, err := m.w.Write(pkt); err != nil {
			return err
		}
	}

	if dataSize%2 == 1 {
		if _, err := m.w.Write([]byte{0}); err != nil {
			return err
		}
	}

	return nil
}

func wavFormatForMediaAttributes(attr media.MediaAttributes) (audioFormat uint16, bitsPerSample uint16, err error) {
	switch attr.Codec {
	case media.CodecPCMA:
		return wavAudioALaw, 8, nil
	case media.CodecPCMU:
		return wavAudioULaw, 8, nil
	case media.CodecLPCM, "":
		switch attr.Audio.Format.Packed() {
		case media.SampleFormatU8:
			return wavAudioPCM, 8, nil
		case media.SampleFormatS16:
			return wavAudioPCM, 16, nil
		case media.SampleFormatS24:
			return wavAudioPCM, 24, nil
		case media.SampleFormatS32:
			return wavAudioPCM, 32, nil
		case media.SampleFormatF32:
			return wavAudioIEEEF, 32, nil
		case media.SampleFormatF64:
			return wavAudioIEEEF, 64, nil
		default:
			return 0, 0, fmt.Errorf("unsupported wav sample format: %s", attr.Audio.Format)
		}
	default:
		return 0, 0, fmt.Errorf("unsupported wav codec: %s", attr.Codec)
	}
}
