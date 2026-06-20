package internal

import (
	"errors"
	"fmt"
	"io"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
	id3 "github.com/godexture/metadata-id3"
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

func NewMuxer(w io.Writer) *Muxer {
	return &Muxer{w: w}
}
func (m *Muxer) 	AddStream(streamInfo media.StreamInfo) (int, error) {
	if m.streamSet {
		return 0, errors.New("mp3 muxer supports only one audio stream")
	}
	if streamInfo.Type != media.MediaAudio {
		return 0, errors.New("mp3 muxer expects an audio stream")
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
	tag, err := id3.Marshal(m.metadata, id3.MarshalOptions{Version: id3.Version2v3})
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
	if err := m.WriteHeader(); err != nil {
		return err
	}
	_, err := m.w.Write(packet.Data())
	return err
}

func (m *Muxer) WriteTrailer() error {
	// MP3はストリーム形式のためトレーラーは不要
	return nil
}
