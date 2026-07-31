package internal

import (
	"errors"
	"fmt"
	"io"
	"time"
)

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
		if totalFrames > 0 && d.vbriHeader.FramesPerEntry > 0 {
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

	d.br.Reset(d.r)
	d.id3Skipped = true
	d.synced = false

	sampleRate := float64(d.streamInfo.MediaAttributes.Audio.SampleRate)
	d.presentationTimestamp = int64(offset.Seconds() * sampleRate)

	return nil
}

func (d *Demuxer) getBitrateBasedOffset(offset time.Duration) (int64, error) {
	if d.bitRate <= 0 {
		return 0, errors.New("mp3 demuxer: unable to seek, unknown bitrate")
	}
	byteOffset := int64(offset.Seconds() * float64(d.bitRate) / 8.0)
	if byteOffset < 0 {
		byteOffset = 0
	}
	return d.firstFrameOffset + byteOffset, nil
}
func getFileSize(r io.ReadSeeker) (size int64, err error) {
	current, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	defer func() {
		if _, restoreErr := r.Seek(current, io.SeekStart); err == nil {
			err = restoreErr
		}
	}()

	size, err = r.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}
	return size, nil
}
