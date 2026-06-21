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

	formatTag, bitsPerSample, err := wavFormatForMediaAttributes(m.stream.MediaAttributes)
	if err != nil {
		return err
	}

	channels := m.stream.MediaAttributes.Audio.ChannelLayout.ChannelCount()
	if channels <= 0 {
		return errors.New("wav muxer requires a valid channel layout")
	}

	sampleRate := m.stream.MediaAttributes.Audio.SampleRate
	if sampleRate <= 0 {
		return errors.New("wav muxer requires a valid sample rate")
	}

	blockAlign := channels * int(bitsPerSample/8)
	byteRate := sampleRate * blockAlign

	var dataSize int
	for _, pkt := range m.packets {
		dataSize += len(pkt)
	}

	pad := 0
	if dataSize%2 == 1 {
		pad = 1
	}

	var out bytes.Buffer
	out.Grow(44 + dataSize + pad)

	if _, err := out.WriteString("RIFF"); err != nil {
		return err
	}

	riffSize := uint32(36 + dataSize + pad)
	if err := binary.Write(&out, binary.LittleEndian, riffSize); err != nil {
		return err
	}

	if _, err := out.WriteString("WAVE"); err != nil {
		return err
	}

	if _, err := out.WriteString("fmt "); err != nil {
		return err
	}
	if err := binary.Write(&out, binary.LittleEndian, uint32(16)); err != nil {
		return err
	}
	if err := binary.Write(&out, binary.LittleEndian, formatTag); err != nil {
		return err
	}
	if err := binary.Write(&out, binary.LittleEndian, uint16(channels)); err != nil {
		return err
	}
	if err := binary.Write(&out, binary.LittleEndian, uint32(sampleRate)); err != nil {
		return err
	}
	if err := binary.Write(&out, binary.LittleEndian, uint32(byteRate)); err != nil {
		return err
	}
	if err := binary.Write(&out, binary.LittleEndian, uint16(blockAlign)); err != nil {
		return err
	}
	if err := binary.Write(&out, binary.LittleEndian, bitsPerSample); err != nil {
		return err
	}

	if _, err := out.WriteString("data"); err != nil {
		return err
	}
	if err := binary.Write(&out, binary.LittleEndian, uint32(dataSize)); err != nil {
		return err
	}

	for _, pkt := range m.packets {
		if _, err := out.Write(pkt); err != nil {
			return err
		}
	}

	if pad == 1 {
		if err := out.WriteByte(0); err != nil {
			return err
		}
	}

	_, err = m.w.Write(out.Bytes())
	return err
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
