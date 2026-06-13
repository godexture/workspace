package internal

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/engine"
	mp3lib "github.com/hajimehoshi/go-mp3"
)

// DecoderConfig はMP3デコーダの設定。
type DecoderConfig struct{}

func (DecoderConfig) NodeConfigaration() {}

// Decoder は mp3 パケット → media.AudioFrame に変換する。
//
// 制約 (go-mp3 ライブラリに由来):
//   - 出力は常に SampleFormatS16 (16-bit signed little-endian)
//   - 出力は常に 2ch Stereo (LayoutStereo2_0)
//   - Mono MP3 をデコードしても Stereo として出力される
type Decoder struct {
	pending *media.Packet
	flushed bool
}

func NewDecoder() *Decoder {
	return &Decoder{}
}

func (d *Decoder) SendPacket(pkt *media.Packet) error {
	if pkt == nil {
		return errors.New("codec-mp3 decoder: received nil packet")
	}
	if d.pending != nil {
		return errors.New("codec-mp3 decoder: has unconsumed packet")
	}
	d.pending = pkt
	return nil
}

func (d *Decoder) ReceiveFrame() (*media.Frame, error) {
	if d.pending == nil {
		if d.flushed {
			return nil, engine.ErrEOF
		}
		return nil, engine.ErrEAGAIN
	}

	pkt := d.pending
	d.pending = nil

	// go-mp3 でデコードする
	// mp3lib.NewDecoder は io.Reader を受け取る
	dec, err := mp3lib.NewDecoder(bytes.NewReader(pkt.Data()))
	if err != nil {
		return nil, fmt.Errorf("codec-mp3 decode: %w", err)
	}

	// デコードした PCM バイト列を取得 (S16 LE Stereo)
	pcmData, err := io.ReadAll(dec)
	if err != nil {
		return nil, fmt.Errorf("codec-mp3 read pcm: %w", err)
	}

	sampleRate := dec.SampleRate()

	// サンプル数を計算: S16 = 2バイト/sample, Stereo = 2ch
	// 総バイト数 / (2 bytes * 2 channels) = サンプル数/チャンネル
	const bytesPerSamplePerChannel = 4 // 2 bytes (S16) * 2 channels
	samples := len(pcmData) / bytesPerSamplePerChannel
	if samples == 0 {
		return nil, engine.ErrEAGAIN
	}

	frame := media.NewAudioFrame(
		media.SampleFormatS16,
		media.LayoutStereo2_0,
		sampleRate,
		samples,
		media.WithAudioPts(pkt.PTS),
	)
	copy(frame.Planes()[0], pcmData)

	var f media.Frame = frame
	return &f, nil
}

func (d *Decoder) Flush() error {
	d.flushed = true
	return nil
}
