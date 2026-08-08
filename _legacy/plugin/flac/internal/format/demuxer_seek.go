package internal

import (
	"fmt"
	"io"
	"time"

	"github.com/godexture/godec/plugin/flac/internal/frame"
)

func (d *Demuxer) Seek(offset time.Duration) error {
	if !d.parsed {
		if _, _, err := d.Analyze(); err != nil {
			return err
		}
	}
	if offset <= 0 {
		return d.seekToStart()
	}

	target := d.targetSample(offset)
	estimate, err := d.estimateByteOffset(target)
	if err != nil {
		return err
	}
	return d.seekToSample(target, estimate)
}

func (d *Demuxer) seekToStart() error {
	if _, err := d.r.Seek(d.audioOffset, io.SeekStart); err != nil {
		return fmt.Errorf("seek FLAC audio frames: %w", err)
	}
	d.scanner = nil
	d.pendingFrame = nil
	d.samplePos = 0
	d.expectedNumber = 0
	d.started = true
	return nil
}

func (d *Demuxer) targetSample(offset time.Duration) uint64 {
	seconds := uint64(offset / time.Second)
	remainder := uint64(offset % time.Second)
	target := seconds*uint64(d.nativeInfo.SampleRate) + remainder*uint64(d.nativeInfo.SampleRate)/uint64(time.Second)
	if d.nativeInfo.TotalSamples > 0 && target >= d.nativeInfo.TotalSamples {
		return d.nativeInfo.TotalSamples - 1
	}
	return target
}

func (d *Demuxer) seekToSample(target uint64, estimate int64) error {
	margin := max(int64(d.nativeInfo.MaxFrameSize), int64(64<<10))
	for {
		start := max(d.audioOffset, estimate-margin)
		data, header, scanner, err := d.locateAt(start)
		if err != nil {
			return err
		}
		samplePos := frame.StartSample(header, d.nativeInfo)
		if samplePos > target && start > d.audioOffset {
			margin = min(margin*2, estimate-d.audioOffset)
			continue
		}
		return d.queueTargetFrame(target, data, header, scanner, samplePos)
	}
}

func (d *Demuxer) queueTargetFrame(target uint64, data []byte, header frame.Header, scanner *frame.Scanner, samplePos uint64) error {
	for samplePos+uint64(header.BlockSize) <= target {
		nextData, nextHeader, err := scanner.Next()
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return fmt.Errorf("flac seek: scan forward: %w", err)
		}
		data, header = nextData, nextHeader
		samplePos = frame.StartSample(header, d.nativeInfo)
	}
	d.scanner = scanner
	d.pendingFrame = &pendingFrame{data: data, header: header}
	d.samplePos = samplePos
	if header.BlockingStrategy {
		d.expectedNumber = header.Number + uint64(header.BlockSize)
	} else {
		d.expectedNumber = header.Number + 1
	}
	d.started = true
	return nil
}

func (d *Demuxer) locateAt(offset int64) ([]byte, frame.Header, *frame.Scanner, error) {
	if _, err := d.r.Seek(offset, io.SeekStart); err != nil {
		return nil, frame.Header{}, nil, fmt.Errorf("seek FLAC audio frames: %w", err)
	}
	reader := io.Reader(d.r)
	if !d.strict {
		reader = io.LimitReader(d.r, d.audioEnd-offset)
	}
	scanner, err := frame.NewScanner(reader, d.nativeInfo, frame.Options{Strict: d.strict, Sync: true})
	if err != nil {
		return nil, frame.Header{}, nil, fmt.Errorf("flac seek: locate frame: %w", err)
	}
	data, header, err := scanner.Next()
	if err != nil {
		return nil, frame.Header{}, nil, fmt.Errorf("flac seek: locate frame: %w", err)
	}
	return data, header, scanner, nil
}

func (d *Demuxer) estimateByteOffset(target uint64) (int64, error) {
	if offset, ok := d.seekTableOffset(target); ok {
		return d.audioOffset + offset, nil
	}
	if d.nativeInfo.TotalSamples == 0 {
		return d.audioOffset, nil
	}
	fileSize, err := d.fileSize()
	if err != nil {
		return 0, err
	}
	audioSize := fileSize - d.audioOffset
	if audioSize <= 0 {
		return d.audioOffset, nil
	}
	return d.audioOffset + int64(float64(target)/float64(d.nativeInfo.TotalSamples)*float64(audioSize)), nil
}

func (d *Demuxer) seekTableOffset(target uint64) (int64, bool) {
	best := -1
	for i, point := range d.seekPoints {
		if point.SampleNumber <= target && (best < 0 || point.SampleNumber > d.seekPoints[best].SampleNumber) {
			best = i
		}
	}
	if best < 0 || d.seekPoints[best].StreamOffset > uint64(^uint64(0)>>1) {
		return 0, false
	}
	return int64(d.seekPoints[best].StreamOffset), true
}

func (d *Demuxer) fileSize() (int64, error) {
	current, err := d.r.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	defer d.r.Seek(current, io.SeekStart)
	return d.r.Seek(0, io.SeekEnd)
}
