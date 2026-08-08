package internal

import (
	"errors"
	"fmt"
	"io"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/domain/metadata"
	"github.com/godexture/godec/plugin/flac/internal/frame"
	"github.com/godexture/godec/plugin/flac/internal/streaminfo"
	vc "github.com/godexture/godec/plugin/vorbiscomment"
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

func NewMuxer(w io.Writer, _ MuxerConfig) *Muxer {
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
	rawBlocks, err := metadataBlocks(m.metadata)
	if err != nil {
		return err
	}
	hasRawVorbisComment := false
	for _, block := range rawBlocks {
		if block.blockType == streaminfo.MetadataTypeVorbisComment {
			hasRawVorbisComment = true
			break
		}
	}
	extraBlocks := make([]metadataBlock, 0, len(rawBlocks)+1)
	if !hasRawVorbisComment {
		extraBlocks = append(extraBlocks, metadataBlock{
			blockType: streaminfo.MetadataTypeVorbisComment,
			payload:   vc.Marshal(m.metadata),
		})
	}
	for _, thumbnail := range metadata.Get[metadata.KeyThumbnail](&m.metadata) {
		extraBlocks = append(extraBlocks, metadataBlock{
			blockType: streaminfo.MetadataTypePicture,
			payload:   vc.MarshalPicture(thumbnail),
		})
	}
	extraBlocks = append(extraBlocks, rawBlocks...)

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
	if packet.Kind == media.PacketKindStreamEnd {
		if err := m.WriteHeader(); err != nil {
			return err
		}
		return m.applyPacketParameters(packet.CodecParameters)
	}
	if packet.Kind != media.PacketKindData {
		return fmt.Errorf("flac muxer unsupported packet kind: %d", packet.Kind)
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

func (m *Muxer) applyPacketParameters(parameters []media.CodecParameters) error {
	for _, param := range parameters {
		if !media.IsCodecParameters[streaminfo.PCMMD5Parameters](param) {
			continue
		}
		if len(param.Data) != len(m.info.MD5) {
			return fmt.Errorf("flac muxer PCM MD5 codec parameter has invalid length: %d", len(param.Data))
		}
		copy(m.info.MD5[:], param.Data)
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
	if err := m.WriteHeader(); err != nil {
		return err
	}
	if !m.headerWritten {
		if err := m.writeHeader(m.info); err != nil {
			return err
		}
	}
	if seeker, ok := m.w.(io.WriteSeeker); ok {
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
