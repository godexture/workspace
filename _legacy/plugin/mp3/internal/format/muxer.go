package internal

import (
	"errors"
	"fmt"
	"io"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/domain/metadata"
	"github.com/godexture/godec/plugin/id3/id3v1"
	"github.com/godexture/godec/plugin/id3/id3v2"
)

// Muxer はMP3パケットをストリームに書き出す。
// MP3はコンテナヘッダーを持たないストリーム形式のため、
// WriteHeader/WriteTrailer は何もしない。
type Muxer struct {
	w          io.Writer
	streamSet  bool
	streamInfo media.StreamInfo
	metadata   metadata.Bundle
	headerDone bool
}

func NewMuxer(w io.Writer, _ MuxerConfig) *Muxer {
	return &Muxer{w: w}
}

func (m *Muxer) AddStream(streamInfo media.StreamInfo) (int, error) {
	if m.streamSet {
		return 0, errors.New("mp3 muxer supports only one audio stream")
	}
	if streamInfo.Type != media.MediaAudio {
		return 0, errors.New("mp3 muxer expects an audio stream")
	}
	if streamInfo.Codec != media.CodecMP3 {
		return 0, fmt.Errorf("mp3 muxer expects codec %q, got %q", media.CodecMP3, streamInfo.Codec)
	}
	m.streamInfo = streamInfo
	m.streamSet = true
	return 0, nil
}

func (m *Muxer) SetMetadata(metadataBundle metadata.Bundle) error {
	m.metadata = metadataBundle
	return nil
}

func (m *Muxer) WriteHeader() error {
	if m.headerDone {
		return nil
	}
	if !m.streamSet {
		return errors.New("mp3 muxer stream is not configured")
	}
	tag, err := id3v2.Marshal(m.metadata, id3v2.MarshalOptions{Version: id3v2.Version3})
	if err != nil {
		return fmt.Errorf("mp3 muxer build id3: %w", err)
	}
	if len(tag) > 0 {
		if _, err := m.w.Write(tag); err != nil {
			return err
		}
	}
	m.headerDone = true
	return nil
}

func (m *Muxer) WritePacket(streamIndex int, packet *media.Packet) error {
	if !m.streamSet {
		return errors.New("mp3 muxer stream is not configured")
	}
	if streamIndex != 0 {
		return fmt.Errorf("mp3 muxer only accepts stream 0, got %d", streamIndex)
	}
	if packet == nil {
		return errors.New("mp3 muxer received nil packet")
	}
	if packet.Kind == media.PacketKindStreamEnd {
		return nil
	}
	if packet.Kind != media.PacketKindData {
		return fmt.Errorf("mp3 muxer unsupported packet kind: %d", packet.Kind)
	}
	if err := m.WriteHeader(); err != nil {
		return err
	}
	_, err := m.w.Write(packet.Data())
	return err
}

func (m *Muxer) WriteTrailer() error {
	tag, err := id3v1.Marshal(m.metadata)
	if err != nil {
		return fmt.Errorf("mp3 muxer build id3v1: %w", err)
	}
	if len(tag) > 0 {
		if _, err := m.w.Write(tag); err != nil {
			return err
		}
	}
	return nil
}
