package internal

import (
	"errors"
	"fmt"
	"io"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
)

// Muxer はMP3パケットをストリームに書き出す。
// MP3はコンテナヘッダーを持たないストリーム形式のため、
// WriteHeader/WriteTrailer は何もしない。
type Muxer struct {
	w          io.Writer
	streamSet  bool
	streamInfo media.StreamInfo
}

func NewMuxer(w io.Writer) *Muxer {
	return &Muxer{w: w}
}
func (m *Muxer) AddStream(streamInfo media.StreamInfo) (int, error) {
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
	// TODO: ID3v2 タグの書き込みをサポートする
	// 参照: https://id3.org/id3v2.3.0
	return nil
}

func (m *Muxer) WriteHeader() error {
	// MP3はストリーム形式のためヘッダーは不要
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
	_, err := m.w.Write(packet.Data())
	return err
}

func (m *Muxer) WriteTrailer() error {
	// MP3はストリーム形式のためトレーラーは不要
	return nil
}
