package internal

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/domain/metadata"
	mediatime "github.com/godexture/godec/core/domain/time"
	"github.com/godexture/godec/plugins/format-mp3/header"
	id3 "github.com/godexture/godec/plugins/metadata-id3"
)

// Demuxer はMP3コンテナを読み込む。
type Demuxer struct {
	r                     io.ReadSeeker
	br                    *bufio.Reader
	streamInfo            media.StreamInfo
	metadataBundle        metadata.Bundle
	parsed                bool
	presentationTimestamp int64
	id3Skipped            bool
	synced                bool

	firstFrameOffset int64
	bitRate          int
	xingHeader       *header.XingHeader
	vbriHeader       *header.VBRIHeader
	duration         time.Duration
	timebase         mediatime.Rational
	freeFormatBytes  int
}

func NewDemuxer(r io.ReadSeeker, _ DemuxerConfig) (*Demuxer, error) {
	if r == nil {
		return nil, errors.New("mp3 demuxer requires a non-nil ReadSeeker")
	}
	return &Demuxer{r: r}, nil
}
func (d *Demuxer) Analyze() ([]media.StreamInfo, metadata.Bundle, error) {
	if d.parsed {
		return []media.StreamInfo{d.streamInfo}, d.metadataBundle, nil
	}

	if _, err := d.r.Seek(0, io.SeekStart); err != nil {
		return nil, metadata.Bundle{}, err
	}

	parsedMetadata, err := id3.ParseReader(d.r)
	if err != nil {
		return nil, metadata.Bundle{}, fmt.Errorf("mp3 parse metadata: %w", err)
	}
	if _, err := d.r.Seek(0, io.SeekStart); err != nil {
		return nil, metadata.Bundle{}, err
	}
	br := bufio.NewReaderSize(d.r, scanWindowBytes)
	id3SkippedBytes, err := SkipID3v2(br)
	if err != nil {
		return nil, metadata.Bundle{}, fmt.Errorf("mp3 skip id3: %w", err)
	}

	d.firstFrameOffset = int64(id3SkippedBytes)

	frameHeader, frameData, freeFormatBytes, err := nextFrameHeader(br, 0)
	if err != nil {
		return nil, metadata.Bundle{}, fmt.Errorf("mp3 analyze: %w", err)
	}
	d.freeFormatBytes = freeFormatBytes

	d.bitRate = frameHeader.BitRate
	d.timebase = mediatime.NewRational(1, int64(frameHeader.SampleRate))

	isMPEG1 := frameHeader.Version == 3
	isMono := frameHeader.ChannelMode == header.ChannelModeMono

	if xh, err := header.ParseXingHeader(frameData, isMPEG1, isMono); err == nil && xh != nil {
		d.xingHeader = xh
		if xh.HasFrames {
			numSamples := int64(xh.Frames) * int64(frameHeader.Samples)
			d.duration = time.Duration(numSamples) * time.Second / time.Duration(frameHeader.SampleRate)
		}
		if xh.HasBytes && xh.HasFrames && d.duration > 0 {
			d.bitRate = int(float64(xh.Bytes) * 8.0 / d.duration.Seconds())
		}
	}
	if vh, err := header.ParseVBRIHeader(frameData); err == nil && vh != nil {
		d.vbriHeader = vh
		numSamples := int64(vh.Frames) * int64(frameHeader.Samples)
		d.duration = time.Duration(numSamples) * time.Second / time.Duration(frameHeader.SampleRate)
		if vh.Bytes > 0 && vh.Frames > 0 && d.duration > 0 {
			d.bitRate = int(float64(vh.Bytes) * 8.0 / d.duration.Seconds())
		}
	}
	if d.duration == 0 && d.bitRate > 0 {
		if size, sizeErr := getFileSize(d.r); sizeErr == nil && size > d.firstFrameOffset {
			seconds := float64(size-d.firstFrameOffset) * 8 / float64(d.bitRate)
			d.duration = time.Duration(seconds * float64(time.Second))
		}
	}

	channelLayout := media.LayoutStereo2_0
	if frameHeader.ChannelMode == header.ChannelModeMono {
		channelLayout = media.LayoutMono1
	}

	d.streamInfo = media.StreamInfo{
		Index:     0,
		Type:      media.MediaAudio,
		IsDefault: true,
		Duration:  d.duration,
		Metadata:  *parsedMetadata,
		MediaAttributes: media.MediaAttributes{
			Codec: media.CodecMP3,
			Audio: media.AudioAttributes{
				SampleRate:    frameHeader.SampleRate,
				Format:        media.SampleFormatF32, // デコード後の形式(codec-mp3に合わせる)
				ChannelLayout: channelLayout,
			},
		},
	}
	d.metadataBundle = *parsedMetadata
	d.parsed = true

	// シークして先頭に戻す
	if _, err := d.r.Seek(0, io.SeekStart); err != nil {
		return nil, metadata.Bundle{}, err
	}
	br.Reset(d.r)
	d.br = br
	d.id3Skipped = false
	d.synced = false

	return []media.StreamInfo{d.streamInfo}, d.metadataBundle, nil
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

	var frameHeader FrameHeader
	var packet *media.Packet
	var err error
	if d.synced {
		var ok bool
		frameHeader, packet, ok, err = readFramePacket(d.br, d.freeFormatBytes)
		if err == nil && !ok {
			frameHeader, packet, d.freeFormatBytes, err = nextFramePacket(d.br, d.freeFormatBytes)
		}
	} else {
		frameHeader, packet, d.freeFormatBytes, err = nextFramePacket(d.br, d.freeFormatBytes)
	}
	if err != nil {
		if err == io.EOF {
			return nil, 0, io.EOF
		}
		return nil, 0, fmt.Errorf("mp3 read packet: %w", err)
	}
	d.synced = true

	packet.MediaType = media.MediaAudio
	packet.StreamIndex = 0
	packet.PTS = media.Pts(d.presentationTimestamp)
	packet.DTS = media.Dts(d.presentationTimestamp)
	packet.Timebase = d.timebase

	samplesPerFrame := frameHeader.Samples
	d.presentationTimestamp += int64(samplesPerFrame)

	return packet, 0, nil
}
