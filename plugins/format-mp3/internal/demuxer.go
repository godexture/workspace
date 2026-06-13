package internal

import (
	"bufio"
	"errors"
	"fmt"
	"io"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
)

// Demuxer はMP3コンテナを読み込む。
type Demuxer struct {
	r          io.ReadSeeker
	br         *bufio.Reader
	streamInfo media.StreamInfo
	meta       metadata.Bundle
	parsed     bool
	pts        int64
	id3Skipped bool
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

	if _, err := d.r.Seek(0, io.SeekStart); err != nil {
		return nil, metadata.Bundle{}, err
	}
	br := bufio.NewReader(d.r)
	if _, err := SkipID3v2(br); err != nil {
		return nil, metadata.Bundle{}, fmt.Errorf("mp3 skip id3: %w", err)
	}

	header, _, err := NextFrameHeader(br)
	if err != nil {
		return nil, metadata.Bundle{}, fmt.Errorf("mp3 analyze: %w", err)
	}

	layout := media.LayoutStereo2_0
	if header.ChannelMode == 3 {
		layout = media.LayoutMono1
	}

	d.streamInfo = media.StreamInfo{
		Index:     0,
		Type:      media.MediaAudio,
		IsDefault: true,
		Metadata:  *metadata.NewBundle(),
		MediaAttributes: media.MediaAttributes{
			Codec: media.CodecMPEG3,
			Audio: media.AudioAttributes{
				CodecID:       media.CodecMPEG3,
				SampleRate:    header.SampleRate,
				Format:        media.SampleFormatS16, // デコード後の形式(codec-mp3に合わせる)
				ChannelLayout: layout,
			},
		},
	}
	d.meta = *metadata.NewBundle()
	d.parsed = true

	// シークして先頭に戻す
	if _, err := d.r.Seek(0, io.SeekStart); err != nil {
		return nil, metadata.Bundle{}, err
	}
	d.br = bufio.NewReader(d.r)
	d.id3Skipped = false

	return []media.StreamInfo{d.streamInfo}, d.meta, nil
}

func (d *Demuxer) ReadPacket() (*media.Packet, int, error) {
	if !d.parsed {
		if _, _, err := d.Analyze(); err != nil {
			return nil, 0, err
		}
	}

	if !d.id3Skipped {
		if _, err := SkipID3v2(d.br); err != nil {
			return nil, 0, fmt.Errorf("mp3 skip id3 on read: %w", err)
		}
		d.id3Skipped = true
	}

	header, data, err := NextFrameHeader(d.br)
	if err != nil {
		if err == io.EOF {
			return nil, 0, io.EOF
		}
		return nil, 0, fmt.Errorf("mp3 read packet: %w", err)
	}

	pkt := media.NewPacket(len(data))
	copy(pkt.Data(), data)
	pkt.MediaType = media.MediaAudio
	pkt.StreamIndex = 0
	pkt.PTS = media.Pts(d.pts)

	samplesPerFrame := 1152
	if header.Version != MPEG1 {
		samplesPerFrame = 576
	}
	d.pts += int64(samplesPerFrame)

	return pkt, 0, nil
}
