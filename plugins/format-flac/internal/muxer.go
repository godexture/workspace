package internal

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
	"github.com/godexture/format-flac/streaminfo"
)

// Muxer writes a native FLAC stream. Packets are expected to contain complete
// FLAC frames, as produced by codec-flac.
type Muxer struct {
	w io.Writer

	streamSet     bool
	stream        media.StreamInfo
	metadata      metadata.Bundle
	headerWritten bool
	closed        bool
}

func NewMuxer(w io.Writer) *Muxer {
	return &Muxer{w: w}
}

func (m *Muxer) AddStream(info media.StreamInfo) (int, error) {
	if m.streamSet {
		return 0, errors.New("flac muxer supports only one audio stream")
	}
	if info.Type != media.MediaAudio {
		return 0, errors.New("flac muxer expects an audio stream")
	}
	if info.MediaAttributes.Codec != media.CodecFLAC {
		return 0, fmt.Errorf("flac muxer expects FLAC packets, got %s", info.MediaAttributes.Codec)
	}
	if _, err := buildStreamInfo(info); err != nil {
		return 0, err
	}

	m.stream = info
	m.streamSet = true
	return 0, nil
}

func (m *Muxer) SetMetadata(meta metadata.Bundle) error {
	m.metadata = meta
	return nil
}

func (m *Muxer) WriteHeader() error {
	if m.headerWritten {
		return nil
	}
	if m.w == nil {
		return errors.New("flac muxer requires a non-nil writer")
	}
	if !m.streamSet {
		return errors.New("flac muxer stream is not configured")
	}

	streamInfoBlock, err := buildStreamInfo(m.stream)
	if err != nil {
		return err
	}
	extraBlocks, err := metadataBlocks(m.metadata)
	if err != nil {
		return err
	}

	var header bytes.Buffer
	header.WriteString(streaminfo.Marker)
	writeMetadataBlockHeader(&header, len(extraBlocks) == 0, streaminfo.MetadataTypeStreamInfo, len(streamInfoBlock))
	header.Write(streamInfoBlock)
	for i, block := range extraBlocks {
		writeMetadataBlockHeader(&header, i == len(extraBlocks)-1, block.blockType, len(block.payload))
		header.Write(block.payload)
	}

	if _, err := m.w.Write(header.Bytes()); err != nil {
		return fmt.Errorf("write FLAC header: %w", err)
	}
	m.headerWritten = true
	return nil
}

func (m *Muxer) WritePacket(streamIndex int, packet *media.Packet) error {
	if !m.streamSet {
		return errors.New("flac muxer stream is not configured")
	}
	if streamIndex != 0 {
		return fmt.Errorf("flac muxer only accepts stream 0, got %d", streamIndex)
	}
	if packet == nil {
		return errors.New("flac muxer received nil packet")
	}
	if m.closed {
		return errors.New("flac muxer is already closed")
	}
	if err := m.WriteHeader(); err != nil {
		return err
	}
	if _, err := m.w.Write(packet.Data()); err != nil {
		return fmt.Errorf("write FLAC frame: %w", err)
	}
	return nil
}

func (m *Muxer) WriteTrailer() error {
	if m.closed {
		return nil
	}
	if !m.streamSet {
		return errors.New("flac muxer stream is not configured")
	}
	if !m.headerWritten {
		return errors.New("flac muxer header was never written")
	}
	m.closed = true
	return nil
}

type metadataBlock struct {
	blockType byte
	payload   []byte
}

func metadataBlocks(meta metadata.Bundle) ([]metadataBlock, error) {
	var blocks []metadataBlock
	for _, raw := range metaRawBlocks(meta) {
		if len(raw) < 4 {
			return nil, errors.New("flac muxer metadata block is shorter than its header")
		}
		blockType := raw[0] & 0x7f
		if blockType == streaminfo.MetadataTypeStreamInfo || blockType > 6 {
			return nil, fmt.Errorf("flac muxer cannot write metadata block type %d", blockType)
		}
		length := int(raw[1])<<16 | int(raw[2])<<8 | int(raw[3])
		if length != len(raw)-4 {
			return nil, fmt.Errorf("flac muxer metadata block length mismatch: header=%d payload=%d", length, len(raw)-4)
		}
		blocks = append(blocks, metadataBlock{blockType: blockType, payload: append([]byte(nil), raw[4:]...)})
	}
	return blocks, nil
}

func metaRawBlocks(meta metadata.Bundle) [][]byte {
	raw, _ := meta.GetRaw(streaminfo.MetadataBlockKey)
	return raw
}

func writeMetadataBlockHeader(w io.Writer, last bool, blockType byte, length int) {
	header := [4]byte{blockType, byte(length >> 16), byte(length >> 8), byte(length)}
	if last {
		header[0] |= 0x80
	}
	_, _ = w.Write(header[:])
}

func buildStreamInfo(stream media.StreamInfo) ([]byte, error) {
	attr := stream.MediaAttributes.Audio
	sampleRate := attr.SampleRate
	channels := attr.ChannelCount()
	bitsPerSample := attr.BitsPerSample

	if raw, ok := stream.Metadata.GetRaw(streaminfo.MetadataKey); ok && len(raw) > 0 {
		parsed, err := streaminfo.Parse(raw[0])
		if err != nil {
			return nil, fmt.Errorf("flac muxer invalid STREAMINFO metadata: %w", err)
		}
		if sampleRate <= 0 {
			sampleRate = parsed.SampleRate
		}
		if channels <= 0 {
			channels = parsed.Channels
		}
		if bitsPerSample <= 0 {
			bitsPerSample = parsed.BitsPerSample
		}
	}
	if bitsPerSample == 0 {
		switch attr.Format.Packed() {
		case media.SampleFormatS16:
			bitsPerSample = 16
		case media.SampleFormatS32:
			bitsPerSample = 32
		default:
			return nil, fmt.Errorf("flac muxer unsupported sample format: %s", attr.Format)
		}
	}

	info := streaminfo.StreamInfo{
		MinBlockSize:  16,
		MaxBlockSize:  65535,
		SampleRate:    sampleRate,
		Channels:      channels,
		BitsPerSample: bitsPerSample,
	}
	if err := streaminfo.Validate(info); err != nil {
		return nil, fmt.Errorf("flac muxer invalid stream attributes: %w", err)
	}

	data := make([]byte, streaminfo.Length)
	binary.BigEndian.PutUint16(data[0:2], info.MinBlockSize)
	binary.BigEndian.PutUint16(data[2:4], info.MaxBlockSize)
	data[10] = byte(info.SampleRate >> 12)
	data[11] = byte(info.SampleRate >> 4)
	data[12] = byte(info.SampleRate<<4) | byte((info.Channels-1)<<1) | byte((info.BitsPerSample-1)>>4)
	data[13] = byte((info.BitsPerSample - 1) << 4)
	return data, nil
}
