package internal

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
	"github.com/godexture/format-mp3/header"
	id3 "github.com/godexture/metadata-id3"
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

	firstFrameOffset int64
	bitRate          int
	xingHeader       *header.XingHeader
	vbriHeader       *header.VBRIHeader
	duration         time.Duration
}

func NewDemuxer(r io.ReadSeeker) (*Demuxer, error) {
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
	br := bufio.NewReader(d.r)
	id3SkippedBytes, err := SkipID3v2(br)
	if err != nil {
		return nil, metadata.Bundle{}, fmt.Errorf("mp3 skip id3: %w", err)
	}

	d.firstFrameOffset = int64(id3SkippedBytes)

	frameHeader, frameData, err := NextFrameHeader(br)
	if err != nil {
		return nil, metadata.Bundle{}, fmt.Errorf("mp3 analyze: %w", err)
	}

	d.bitRate = frameHeader.BitRate

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

	channelLayout := media.LayoutStereo2_0
	if frameHeader.ChannelMode == header.ChannelModeMono {
		channelLayout = media.LayoutMono1
	}

	d.streamInfo = media.StreamInfo{
		Index:     0,
		Type:      media.MediaAudio,
		IsDefault: true,
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
	d.br = bufio.NewReader(d.r)
	d.id3Skipped = false

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

	frameHeader, data, err := NextFrameHeader(d.br)
	if err != nil {
		if err == io.EOF {
			return nil, 0, io.EOF
		}
		return nil, 0, fmt.Errorf("mp3 read packet: %w", err)
	}

	packet := media.NewPacket(len(data))
	copy(packet.Data(), data)
	packet.MediaType = media.MediaAudio
	packet.StreamIndex = 0
	packet.PTS = media.Pts(d.presentationTimestamp)

	samplesPerFrame := frameHeader.Samples
	d.presentationTimestamp += int64(samplesPerFrame)

	return packet, 0, nil
}

func (d *Demuxer) Seek(offset time.Duration) error {
	if !d.parsed {
		if _, _, err := d.Analyze(); err != nil {
			return err
		}
	}

	var targetOffset int64

	if d.xingHeader != nil && d.xingHeader.HasTOC && d.duration > 0 {
		totalBytes := int64(d.xingHeader.Bytes)
		if totalBytes == 0 {
			if fileSize, err := getFileSize(d.r); err == nil {
				totalBytes = fileSize - d.firstFrameOffset
			}
		}

		if totalBytes > 0 {
			percent := float64(offset) / float64(d.duration) * 100.0
			if percent < 0 {
				percent = 0
			}
			if percent > 100 {
				percent = 100
			}

			index := int(percent)
			if index > 99 {
				index = 99
			}
			fraction := percent - float64(index)

			valStart := float64(d.xingHeader.TOC[index])
			valEnd := 256.0
			if index < 99 {
				valEnd = float64(d.xingHeader.TOC[index+1])
			}

			val := valStart + (valEnd-valStart)*fraction
			byteOffset := int64((val / 256.0) * float64(totalBytes))
			targetOffset = d.firstFrameOffset + byteOffset
		} else {
			var err error
			targetOffset, err = d.getBitrateBasedOffset(offset)
			if err != nil {
				return err
			}
		}
	} else if d.vbriHeader != nil && len(d.vbriHeader.TOC) > 0 && d.duration > 0 {
		totalFrames := float64(d.vbriHeader.Frames)
		if totalFrames > 0 {
			durationPerEntry := (float64(d.vbriHeader.FramesPerEntry) / totalFrames) * float64(d.duration)
			t := float64(offset)
			entryIndex := int(t / durationPerEntry)
			fraction := (t - float64(entryIndex)*durationPerEntry) / durationPerEntry

			if entryIndex >= len(d.vbriHeader.TOC) {
				entryIndex = len(d.vbriHeader.TOC) - 1
				fraction = 1.0
			}
			if entryIndex < 0 {
				entryIndex = 0
				fraction = 0.0
			}

			var startOffset uint32 = 0
			if entryIndex > 0 {
				startOffset = d.vbriHeader.TOC[entryIndex-1]
			}

			endOffset := d.vbriHeader.TOC[entryIndex]

			byteOffset := float64(startOffset) + float64(endOffset-startOffset)*fraction
			targetOffset = d.firstFrameOffset + int64(byteOffset)
		} else {
			var err error
			targetOffset, err = d.getBitrateBasedOffset(offset)
			if err != nil {
				return err
			}
		}
	} else {
		var err error
		targetOffset, err = d.getBitrateBasedOffset(offset)
		if err != nil {
			return err
		}
	}

	if _, err := d.r.Seek(targetOffset, io.SeekStart); err != nil {
		return fmt.Errorf("mp3 seek: %w", err)
	}

	d.br = bufio.NewReader(d.r)
	d.id3Skipped = true

	sampleRate := float64(d.streamInfo.MediaAttributes.Audio.SampleRate)
	d.presentationTimestamp = int64(offset.Seconds() * sampleRate)

	return nil
}

func (d *Demuxer) getBitrateBasedOffset(offset time.Duration) (int64, error) {
	if d.bitRate <= 0 {
		return 0, errors.New("mp3 demuxer: unable to seek, unknown bitrate")
	}
	byteOffset := int64(offset.Seconds() * float64(d.bitRate) / 8.0)
	return d.firstFrameOffset + byteOffset, nil
}
func getFileSize(r io.ReadSeeker) (int64, error) {
	current, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	defer r.Seek(current, io.SeekStart)

	size, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}
	return size, nil
}
