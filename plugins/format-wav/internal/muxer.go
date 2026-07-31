package internal

import (
	"errors"
	"fmt"
	"io"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/domain/metadata"
)

type Muxer struct {
	w io.Writer

	streamSet     bool
	stream        media.StreamInfo
	meta          metadata.Bundle
	closed        bool
	headerWritten bool

	// Configuration Options
	forceRF64 bool

	// Seekable streaming mode
	seekable   bool
	dataSize   uint64
	headerSize int64
	startPos   int64
}

type MuxerConfig struct {
	ForceRF64 bool `name:"force-rf64" help:"Always write RF64 output"`
}

func NewMuxer(w io.Writer, config MuxerConfig) *Muxer {
	return &Muxer{w: w, forceRF64: config.ForceRF64}
}

func (m *Muxer) AddStream(info media.StreamInfo) (int, error) {
	if m.streamSet {
		return 0, errors.New("wav muxer supports only one audio stream")
	}

	if info.Type != media.MediaAudio {
		return 0, errors.New("wav muxer expects an audio stream")
	}

	isCompressed := info.MediaAttributes.Codec == media.CodecMSADPCM || info.MediaAttributes.Codec == media.CodecIMAADPCM || info.MediaAttributes.Codec == media.CodecMP3 || info.MediaAttributes.Codec == media.CodecGSM
	if info.MediaAttributes.Audio.Format == media.SampleFormatUnknown && !isCompressed {
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
	if m.headerWritten {
		return errors.New("wav muxer header already written")
	}

	if seeker, ok := m.w.(io.Seeker); ok {
		pos, err := seeker.Seek(0, io.SeekCurrent)
		if err != nil {
			m.seekable = false
		} else {
			m.seekable = true
			m.startPos = pos
		}

		headerBytes, err := buildWAVHeader(m.stream.MediaAttributes, 0, 0, m.forceRF64)
		if err != nil {
			return err
		}

		m.headerSize = int64(len(headerBytes))
		if _, err := m.w.Write(headerBytes); err != nil {
			return err
		}
	} else {
		m.seekable = false
		// For non-seekable streams, write header with unknown size immediately and do not buffer in memory
		headerBytes, err := buildWAVHeader(m.stream.MediaAttributes, ^uint64(0), 0, m.forceRF64)
		if err != nil {
			return err
		}
		if _, err := m.w.Write(headerBytes); err != nil {
			return err
		}
	}
	m.headerWritten = true

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
	if pkt.Kind == media.PacketKindStreamEnd {
		return nil
	}
	if pkt.Kind != media.PacketKindData {
		return fmt.Errorf("wav muxer unsupported packet kind: %d", pkt.Kind)
	}

	if !m.headerWritten {
		if err := m.WriteHeader(); err != nil {
			return err
		}
	}

	n, err := m.w.Write(pkt.Data())
	if err != nil {
		return err
	}
	m.dataSize += uint64(n)
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

	if !m.headerWritten {
		return errors.New("wav muxer header was never written")
	}

	if m.dataSize%2 == 1 {
		if _, err := m.w.Write([]byte{0}); err != nil {
			return err
		}
	}

	var trailerBytes []byte
	if listChunk := buildListInfoChunk(m.meta); len(listChunk) > 0 {
		trailerBytes = append(trailerBytes, listChunk...)
	}
	if id3Chunk := buildID3Chunk(m.meta); len(id3Chunk) > 0 {
		trailerBytes = append(trailerBytes, id3Chunk...)
	}
	if cueChunk := buildRawChunk(wavTagCue, m.meta); len(cueChunk) > 0 {
		trailerBytes = append(trailerBytes, cueChunk...)
	}
	if smplChunk := buildRawChunk(wavTagSmpl, m.meta); len(smplChunk) > 0 {
		trailerBytes = append(trailerBytes, smplChunk...)
	}

	if len(trailerBytes) > 0 {
		if _, err := m.w.Write(trailerBytes); err != nil {
			return err
		}
	}

	if m.seekable {
		if seeker, ok := m.w.(io.Seeker); ok {
			headerBytes, err := buildWAVHeader(m.stream.MediaAttributes, m.dataSize, uint64(len(trailerBytes)), m.forceRF64)
			if err != nil {
				return err
			}

			if int64(len(headerBytes)) != m.headerSize {
				return fmt.Errorf("wav header size changed (original: %d, new: %d), cannot overwrite in streaming mode", m.headerSize, len(headerBytes))
			}

			if _, err := seeker.Seek(m.startPos, io.SeekStart); err != nil {
				return fmt.Errorf("seek to start of wav file failed: %w", err)
			}

			if _, err := m.w.Write(headerBytes); err != nil {
				return fmt.Errorf("overwrite wav header failed: %w", err)
			}
		}
	}

	return nil
}
