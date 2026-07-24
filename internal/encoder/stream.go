package encoder

import (
	"errors"
	"fmt"

	"github.com/godexture/codec-flac/internal/config"
	"github.com/godexture/core/domain/media"
)

func (e *Encoder) configureStream(sampleRate, channels, bitsPerSample int) error {
	if e.config.StreamableSubset && e.config.BlockSize > streamableMaxBlockSize(sampleRate) {
		return fmt.Errorf("FLAC streamable-subset block size %d exceeds %d at %d Hz", e.config.BlockSize, streamableMaxBlockSize(sampleRate), sampleRate)
	}

	if e.config.SampleRate() > 0 && sampleRate != e.config.SampleRate() {
		return fmt.Errorf("FLAC encoder sample rate mismatch: got %d, want %d", sampleRate, e.config.SampleRate())
	}
	if e.config.Channels() > 0 && channels != e.config.Channels() {
		return fmt.Errorf("FLAC encoder channel count mismatch: got %d, want %d", channels, e.config.Channels())
	}
	if e.config.BitsPerSample() > 0 && bitsPerSample != e.config.BitsPerSample() {
		return fmt.Errorf("FLAC encoder bit depth mismatch: got %d, want %d", bitsPerSample, e.config.BitsPerSample())
	}

	if !e.streamSet {
		e.sampleRate = sampleRate
		e.channels = channels
		e.bitsPerSample = bitsPerSample
		e.buffer = make([][]int64, channels)
		e.blockView = make([][]int64, channels)
		e.streamSet = true
		return nil
	}
	if sampleRate != e.sampleRate {
		return fmt.Errorf("FLAC encoder sample rate changed: got %d, want %d", sampleRate, e.sampleRate)
	}
	if channels != e.channels {
		return fmt.Errorf("FLAC encoder channel count changed: got %d, want %d", channels, e.channels)
	}
	if bitsPerSample != e.bitsPerSample {
		return fmt.Errorf("FLAC encoder bit depth changed: got %d, want %d", bitsPerSample, e.bitsPerSample)
	}
	return nil
}

func (e *Encoder) dropBuffered(n int) {
	e.bufferStart += n
	e.buffered -= n
	if e.buffered == 0 {
		for ch := range e.buffer {
			e.buffer[ch] = e.buffer[ch][:0]
		}
		e.bufferStart = 0
	}
}

func (e *Encoder) currentBlock(samples int) [][]int64 {
	for ch := range e.buffer {
		e.blockView[ch] = e.buffer[ch][e.bufferStart : e.bufferStart+samples]
	}
	return e.blockView
}

func (e *Encoder) audioFrameParameters(frame *media.AudioFrame) (int, int, int, error) {
	if frame.Format.IsPlanar() {
		return 0, 0, 0, fmt.Errorf("FLAC encoder does not support planar input format: %s", frame.Format)
	}
	format := frame.Format.Packed()
	bitsPerSample := e.config.BitsPerSample()
	if bitsPerSample <= 0 {
		bitsPerSample = frame.BitsPerSample
	}
	if bitsPerSample <= 0 {
		bitsPerSample = config.BitDepthFromSampleFormat(format)
	}
	if format == media.SampleFormatS16 && (bitsPerSample < 4 || bitsPerSample > 16) {
		return 0, 0, 0, fmt.Errorf("S16 FLAC input requires 4..16 bits per sample, got %d", bitsPerSample)
	}
	if format == media.SampleFormatS24 && (bitsPerSample < 17 || bitsPerSample > 24) {
		return 0, 0, 0, fmt.Errorf("S24 FLAC input requires 17..24 bits per sample, got %d", bitsPerSample)
	}
	if format == media.SampleFormatS32 && (bitsPerSample < 17 || bitsPerSample > 32) {
		return 0, 0, 0, fmt.Errorf("S32 FLAC input requires 17..32 bits per sample, got %d", bitsPerSample)
	}
	if format != media.SampleFormatS16 && format != media.SampleFormatS24 && format != media.SampleFormatS32 {
		return 0, 0, 0, fmt.Errorf("unsupported FLAC input format: %s", frame.Format)
	}
	channels := frame.Layout.ChannelCount()
	if channels < 1 || channels > 8 {
		return 0, 0, 0, fmt.Errorf("unsupported FLAC channel count: %d", channels)
	}
	if frame.SampleRate <= 0 {
		return 0, 0, 0, fmt.Errorf("invalid FLAC sample rate: %d", frame.SampleRate)
	}
	if frame.Samples <= 0 {
		return 0, 0, 0, fmt.Errorf("invalid FLAC sample count: %d", frame.Samples)
	}
	planes := frame.Planes()
	if len(planes) == 0 {
		return 0, 0, 0, errors.New("FLAC input has no audio plane")
	}
	wantBytes := frame.Samples * channels * format.BytesPerSample()
	if len(planes[0]) < wantBytes {
		return 0, 0, 0, fmt.Errorf("FLAC input plane is too short: got %d, want %d", len(planes[0]), wantBytes)
	}
	return frame.SampleRate, channels, bitsPerSample, nil
}

func (e *Encoder) appendAudioFrame(frame *media.AudioFrame) error {
	e.ensureBufferSpace(frame.Samples)
	format := frame.Format.Packed()
	channels := len(e.buffer)
	plane := frame.Planes()[0]
	minValue := -(int64(1) << uint(e.bitsPerSample-1))
	maxValue := (int64(1) << uint(e.bitsPerSample-1)) - 1
	writeStart := len(e.buffer[0])
	for ch := range e.buffer {
		e.buffer[ch] = e.buffer[ch][:writeStart+frame.Samples]
	}

	var err error
	switch format {
	case media.SampleFormatS16:
		err = deinterleaveS16(e.buffer, plane, writeStart, frame.Samples, channels, minValue, maxValue, e.bitsPerSample)
	case media.SampleFormatS24:
		err = deinterleaveS24(e.buffer, plane, writeStart, frame.Samples, channels, minValue, maxValue, e.bitsPerSample)
	case media.SampleFormatS32:
		err = deinterleaveS32(e.buffer, plane, writeStart, frame.Samples, channels, minValue, maxValue, e.bitsPerSample)
	default:
		err = fmt.Errorf("unsupported FLAC input format: %s", format)
	}
	if err != nil {
		for channel := range e.buffer {
			e.buffer[channel] = e.buffer[channel][:writeStart]
		}
		return err
	}

	for ch := range e.buffer {
		e.blockView[ch] = e.buffer[ch][writeStart : writeStart+frame.Samples]
	}
	if format.BytesPerSample() == (e.bitsPerSample+7)/8 {
		e.md5.WritePacked(plane[:frame.Samples*channels*format.BytesPerSample()])
	} else {
		e.md5.Write(e.blockView, e.bitsPerSample)
	}
	e.buffered += frame.Samples
	return nil
}

func (e *Encoder) ensureBufferSpace(additional int) {
	if cap(e.buffer[0])-len(e.buffer[0]) >= additional {
		return
	}
	if e.bufferStart > 0 {
		for ch := range e.buffer {
			copy(e.buffer[ch], e.buffer[ch][e.bufferStart:e.bufferStart+e.buffered])
			e.buffer[ch] = e.buffer[ch][:e.buffered]
		}
		e.bufferStart = 0
	}
	for ch := range e.buffer {
		if cap(e.buffer[ch])-len(e.buffer[ch]) < additional {
			grown := make([]int64, len(e.buffer[ch]), len(e.buffer[ch])+additional+e.config.BlockSize)
			copy(grown, e.buffer[ch])
			e.buffer[ch] = grown
		}
	}
}
