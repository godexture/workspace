package internal

import (
	"errors"
	"fmt"
	"io"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
	"github.com/godexture/format-flac/frame"
	"github.com/godexture/format-flac/streaminfo"
)

// Muxer writes a native FLAC stream. Packets are expected to contain complete
// FLAC frames, as produced by codec-flac.
type Muxer struct {
	w io.Writer

	streamSet      bool
	stream         media.StreamInfo
	metadata       metadata.Bundle
	info           streaminfo.StreamInfo
	extraBlocks    []metadataBlock
	headerPrepared bool
	headerWritten  bool
	closed         bool

	totalSamples     uint64
	minBlockSize     uint16
	maxBlockSize     uint16
	minFrameSize     uint32
	maxFrameSize     uint32
	pendingBlockSize uint16
	frameCount       uint64
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
	if m.headerPrepared {
		return nil
	}
	if m.w == nil {
		return errors.New("flac muxer requires a non-nil writer")
	}
	if !m.streamSet {
		return errors.New("flac muxer stream is not configured")
	}

	info, err := buildStreamInfo(m.stream)
	if err != nil {
		return err
	}
	extraBlocks, err := metadataBlocks(m.metadata)
	if err != nil {
		return err
	}

	m.info = info
	m.extraBlocks = extraBlocks
	m.headerPrepared = true
	return nil
}

func (m *Muxer) writeHeader(info streaminfo.StreamInfo) error {
	if err := writeAll(m.w, []byte(streaminfo.Marker)); err != nil {
		return fmt.Errorf("write FLAC marker: %w", err)
	}
	streamInfoBlock := streaminfo.Encode(info)
	if err := writeMetadataBlockHeader(m.w, len(m.extraBlocks) == 0, streaminfo.MetadataTypeStreamInfo, len(streamInfoBlock)); err != nil {
		return fmt.Errorf("write FLAC STREAMINFO header: %w", err)
	}
	if err := writeAll(m.w, streamInfoBlock); err != nil {
		return fmt.Errorf("write FLAC STREAMINFO: %w", err)
	}
	for i, block := range m.extraBlocks {
		if err := writeMetadataBlockHeader(m.w, i == len(m.extraBlocks)-1, block.blockType, len(block.payload)); err != nil {
			return fmt.Errorf("write FLAC metadata header: %w", err)
		}
		if err := writeAll(m.w, block.payload); err != nil {
			return fmt.Errorf("write FLAC metadata block: %w", err)
		}
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
	parsedFrame, err := frame.ParseHeader(packet.Data(), m.info)
	if err != nil {
		return fmt.Errorf("parse FLAC frame header: %w", err)
	}
	if !m.headerWritten {
		info := m.info
		if !parsedFrame.BlockingStrategy {
			blockSize := streamInfoBlockSize(parsedFrame.BlockSize)
			info.MinBlockSize = blockSize
			info.MaxBlockSize = blockSize
		}
		if err := m.writeHeader(info); err != nil {
			return err
		}
	}
	if err := writeAll(m.w, packet.Data()); err != nil {
		return fmt.Errorf("write FLAC frame: %w", err)
	}
	m.recordFrame(parsedFrame.BlockSize, len(packet.Data()))
	return nil
}

func (m *Muxer) WriteTrailer() error {
	if m.closed {
		return nil
	}
	if !m.streamSet {
		return errors.New("flac muxer stream is not configured")
	}
	if err := m.WriteHeader(); err != nil {
		return err
	}
	if !m.headerWritten {
		if err := m.writeHeader(m.info); err != nil {
			return err
		}
	}
	if seeker, ok := m.w.(io.WriteSeeker); ok && m.frameCount > 0 {
		end, err := seeker.Seek(0, io.SeekCurrent)
		if err != nil {
			return fmt.Errorf("seek FLAC output end: %w", err)
		}
		if _, err := seeker.Seek(8, io.SeekStart); err != nil {
			return fmt.Errorf("seek FLAC STREAMINFO: %w", err)
		}
		if err := writeAll(seeker, streaminfo.Encode(m.finalStreamInfo())); err != nil {
			return fmt.Errorf("rewrite FLAC STREAMINFO: %w", err)
		}
		if _, err := seeker.Seek(end, io.SeekStart); err != nil {
			return fmt.Errorf("restore FLAC output position: %w", err)
		}
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
		blocks = append(blocks, metadataBlock{blockType: blockType, payload: raw[4:]})
	}
	return blocks, nil
}

func metaRawBlocks(meta metadata.Bundle) [][]byte {
	raw, _ := meta.GetRaw(streaminfo.MetadataBlockKey)
	return raw
}

func writeMetadataBlockHeader(w io.Writer, last bool, blockType byte, length int) error {
	header := [4]byte{blockType, byte(length >> 16), byte(length >> 8), byte(length)}
	if last {
		header[0] |= 0x80
	}
	return writeAll(w, header[:])
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

func (m *Muxer) recordFrame(blockSize, frameSize int) {
	blockSizeValue := streamInfoBlockSize(blockSize)
	frameSizeValue := uint32(frameSize)
	if m.frameCount == 0 {
		m.minBlockSize = blockSizeValue
		m.maxBlockSize = blockSizeValue
		m.minFrameSize = frameSizeValue
		m.maxFrameSize = frameSizeValue
	} else {
		if m.pendingBlockSize < m.minBlockSize {
			m.minBlockSize = m.pendingBlockSize
		}
		if blockSizeValue > m.maxBlockSize {
			m.maxBlockSize = blockSizeValue
		}
		if frameSizeValue < m.minFrameSize {
			m.minFrameSize = frameSizeValue
		}
		if frameSizeValue > m.maxFrameSize {
			m.maxFrameSize = frameSizeValue
		}
	}
	m.pendingBlockSize = blockSizeValue
	m.totalSamples += uint64(blockSize)
	m.frameCount++
}

func (m *Muxer) finalStreamInfo() streaminfo.StreamInfo {
	info := m.info
	info.MinBlockSize = m.minBlockSize
	info.MaxBlockSize = m.maxBlockSize
	info.MinFrameSize = m.minFrameSize
	info.MaxFrameSize = m.maxFrameSize
	info.TotalSamples = m.totalSamples
	return info
}

func streamInfoBlockSize(blockSize int) uint16 {
	if blockSize < 16 {
		return 16
	}
	return uint16(blockSize)
}

func buildStreamInfo(stream media.StreamInfo) (streaminfo.StreamInfo, error) {
	attr := stream.MediaAttributes.Audio
	sampleRate := attr.SampleRate
	channels := attr.ChannelCount()
	bitsPerSample := attr.BitsPerSample

	var inherited streaminfo.StreamInfo
	if raw, ok := stream.Metadata.GetRaw(streaminfo.MetadataKey); ok && len(raw) > 0 {
		parsed, err := streaminfo.Parse(raw[0])
		if err != nil {
			return streaminfo.StreamInfo{}, fmt.Errorf("flac muxer invalid STREAMINFO metadata: %w", err)
		}
		inherited = parsed
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
			return streaminfo.StreamInfo{}, fmt.Errorf("flac muxer unsupported sample format: %s", attr.Format)
		}
	}

	info := streaminfo.StreamInfo{
		MinBlockSize:  16,
		MaxBlockSize:  65535,
		SampleRate:    sampleRate,
		Channels:      channels,
		BitsPerSample: bitsPerSample,
		TotalSamples:  inherited.TotalSamples,
		MD5:           inherited.MD5,
	}
	if err := streaminfo.Validate(info); err != nil {
		return streaminfo.StreamInfo{}, fmt.Errorf("flac muxer invalid stream attributes: %w", err)
	}
	return info, nil
}
