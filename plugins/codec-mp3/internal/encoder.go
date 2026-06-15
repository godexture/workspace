package internal

import (
	"errors"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/engine"
)

// EncoderConfig はMP3エンコーダの設定。
type EncoderConfig struct {
	// Bitrate はMP3のビットレート (kbps)。デフォルト: 128
	// TODO: エンコーダ実装時に使用する
	Bitrate int
}

func (EncoderConfig) NodeConfigaration() {}

// Encoder は media.AudioFrame → MP3 パケットに変換する。
//
// 現在の実装状況:
//   - このエンコーダは STUB です。SendFrame は受け付けますが、
//     ReceivePacket は常に engine.ErrEAGAIN を返します。
//
// TODO: 以下のいずれかで実装する:
//   - github.com/braheezy/shine-mp3 (pure Go, ライセンス確認要)
//   - github.com/git-jiadong/go-lame (LAME バインディング, CGO必要)
//   - 外部 ffmpeg/lame CLI 呼び出し
//
// 実装時の注意:
//   - 入力フレームの Format が SampleFormatS16 であることを確認する
//   - Stereo (2ch) のみサポート (Mono は要変換)
//   - Bitrate オプションを EncoderConfig から読む
type Encoder struct {
	config    EncoderConfig
	isFlushed bool
}

func NewEncoder(config EncoderConfig) *Encoder {
	if config.Bitrate <= 0 {
		config.Bitrate = 128
	}
	return &Encoder{config: config}
}
func (e *Encoder) SendFrame(audioFrame *media.Frame) error {
	if audioFrame == nil || *audioFrame == nil {
		return errors.New("codec-mp3 encoder: received nil frame")
	}
	// stub: フレームを受け取るが、エンコードは実装されていない
	return nil
}

func (e *Encoder) ReceivePacket() (*media.Packet, error) {
	if e.isFlushed {
		return nil, engine.ErrEOF
	}
	// stub: 常に ErrEAGAIN を返す (未実装)
	return nil, engine.ErrEAGAIN
}

func (e *Encoder) Flush() error {
	e.isFlushed = true
	return nil
}
