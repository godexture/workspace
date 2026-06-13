package internal

import (
	"errors"
	"fmt"
	"io"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
	mp3lib "github.com/hajimehoshi/go-mp3"
)

// Demuxer はMP3コンテナを読み込む。
// 注意: go-mp3 ライブラリの制約により、デコード後の出力は常に
// 16-bit signed LE, 2-channel (Stereo) PCM となる。
// Mono MP3 の場合でも StreamInfo は LayoutStereo2_0 となる。
type Demuxer struct {
	r          io.ReadSeeker
	streamInfo media.StreamInfo
	meta       metadata.Bundle
	parsed     bool
	sent       bool
}

func NewDemuxer(r io.ReadSeeker) (*Demuxer, error) {
	if r == nil {
		return nil, errors.New("mp3 demuxer requires a non-nil ReadSeeker")
	}
	return &Demuxer{r: r}, nil
}

func (d *Demuxer) Analyze() ([]media.StreamInfo, metadata.Bundle, error) {
	if d.parsed {
		return []media.StreamInfo{d.streamInfo}, d.meta, nil
	}

	// go-mp3 で先頭フレームを解析してサンプルレートを取得する
	if _, err := d.r.Seek(0, io.SeekStart); err != nil {
		return nil, metadata.Bundle{}, err
	}

	dec, err := mp3lib.NewDecoder(d.r)
	if err != nil {
		return nil, metadata.Bundle{}, fmt.Errorf("mp3 analyze: %w", err)
	}

	sampleRate := dec.SampleRate()

	// シークして先頭に戻す (ReadPacket で全体を読むため)
	if _, err := d.r.Seek(0, io.SeekStart); err != nil {
		return nil, metadata.Bundle{}, err
	}

	// go-mp3 は常に Stereo S16 を出力するため、
	// StreamInfo のコーデックは "mpeg3" とし、レイアウトはステレオで固定する。
	// デコーダ (codec-mp3) がこのコーデックを受け入れて変換する。
	d.streamInfo = media.StreamInfo{
		Index:     0,
		Type:      media.MediaAudio,
		IsDefault: true,
		Metadata:  *metadata.NewBundle(),
		MediaAttributes: media.MediaAttributes{
			Codec: media.CodecMPEG3, // "mpeg3"
			Audio: media.AudioAttributes{
				CodecID:       media.CodecMPEG3,
				SampleRate:    sampleRate,
				Format:        media.SampleFormatS16, // デコード後の形式
				ChannelLayout: media.LayoutStereo2_0, // go-mp3 は常にStereo
			},
		},
	}
	d.meta = *metadata.NewBundle()
	d.parsed = true

	return []media.StreamInfo{d.streamInfo}, d.meta, nil
}

func (d *Demuxer) ReadPacket() (*media.Packet, int, error) {
	if !d.parsed {
		if _, _, err := d.Analyze(); err != nil {
			return nil, 0, err
		}
	}

	if d.sent {
		return nil, 0, io.EOF
	}

	// ファイル先頭から全バイトを読み込む
	if _, err := d.r.Seek(0, io.SeekStart); err != nil {
		return nil, 0, fmt.Errorf("mp3 seek: %w", err)
	}

	raw, err := io.ReadAll(d.r)
	if err != nil {
		return nil, 0, fmt.Errorf("mp3 read: %w", err)
	}

	pkt := media.NewPacket(len(raw))
	copy(pkt.Data(), raw)
	pkt.MediaType = media.MediaAudio
	pkt.StreamIndex = 0

	d.sent = true
	return pkt, 0, nil
}
