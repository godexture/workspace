package encoder

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/godexture/codec-flac/internal/flac"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/format-flac/streaminfo"
	"github.com/godexture/sdk/bits"
	"github.com/godexture/sdk/engine"
)

type Encoder struct {
	config  flac.EncoderConfig
	windows windowSet

	pendingQueue []*media.Packet
	flushed      bool

	buffer        [][]int64
	bufferStart   int
	blockView     [][]int64
	buffered      int
	bufferPTS     media.Pts
	streamSet     bool
	sampleRate    int
	channels      int
	bitsPerSample int
	frameNumber   uint64
	sampleNumber  uint64
	writer        bits.Writer
	md5           *flac.PCMMD5
}

func NewEncoder(stream media.StreamInfo, cfg flac.EncoderConfig) (*Encoder, error) {
	cfg = flac.MergeEncoderConfigForFactory(cfg, stream)
	err := cfg.Validate()
	if err != nil {
		return nil, err
	}
	return &Encoder{config: cfg, windows: newWindowSet(cfg.Apodizations), md5: flac.NewPCMMD5()}, nil
}

func (e *Encoder) SendFrame(frame *media.Frame) error {
	if frame == nil || *frame == nil {
		return errors.New("flac encoder received nil frame")
	}
	if e.flushed {
		return engine.ErrEOF
	}

	af, ok := (*frame).(*media.AudioFrame)
	if !ok {
		return errors.New("flac encoder expected *media.AudioFrame")
	}

	sampleRate, channels, bitsPerSample, err := e.audioFrameParameters(af)
	if err != nil {
		return err
	}
	if err := e.configureStream(sampleRate, channels, bitsPerSample); err != nil {
		return err
	}

	if e.buffered == 0 {
		e.bufferPTS = af.Pts()
	}
	if err := e.appendAudioFrame(af); err != nil {
		return err
	}
	return e.emitFullBlocks()
}

func (e *Encoder) ReceivePacket() (*media.Packet, error) {
	if len(e.pendingQueue) == 0 {
		if e.flushed {
			return nil, engine.ErrEOF
		}
		return nil, engine.ErrEAGAIN
	}
	pkt := e.pendingQueue[0]
	e.pendingQueue[0] = nil
	e.pendingQueue = e.pendingQueue[1:]
	if len(e.pendingQueue) == 0 {
		e.pendingQueue = nil
	}
	return pkt, nil
}

func (e *Encoder) Flush() error {
	if e.flushed {
		return nil
	}
	if e.buffered > 0 {
		if err := e.enqueueBlock(e.currentBlock(e.buffered), e.bufferPTS, nil); err != nil {
			return err
		}
		e.dropBuffered(e.buffered)
	}
	e.flushed = true
	sum := e.MD5()
	e.pendingQueue = append(e.pendingQueue, media.NewPacketEvent(media.PacketKindStreamEnd, 0, []media.CodecParameters{
		media.NewCodecParameters[streaminfo.PCMMD5Parameters](sum[:]),
	}))
	return nil
}

func (e *Encoder) MD5() [16]byte {
	return e.md5.Sum()
}

func (e *Encoder) configureStream(sampleRate, channels, bitsPerSample int) error {
	if e.config.StreamableSubset && e.config.BlockSize > streamableMaxBlockSize(sampleRate) {
		return fmt.Errorf("FLAC streamable-subset block size %d exceeds %d at %d Hz", e.config.BlockSize, streamableMaxBlockSize(sampleRate), sampleRate)
	}
	if e.config.SampleRate > 0 && sampleRate != e.config.SampleRate {
		return fmt.Errorf("FLAC encoder sample rate mismatch: got %d, want %d", sampleRate, e.config.SampleRate)
	}
	if e.config.Channels > 0 && channels != e.config.Channels {
		return fmt.Errorf("FLAC encoder channel count mismatch: got %d, want %d", channels, e.config.Channels)
	}
	if e.config.BitsPerSample > 0 && bitsPerSample != e.config.BitsPerSample {
		return fmt.Errorf("FLAC encoder bit depth mismatch: got %d, want %d", bitsPerSample, e.config.BitsPerSample)
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

func (e *Encoder) emitFullBlocks() error {
	for e.buffered >= e.config.BlockSize {
		if err := e.enqueueFullBlock(e.currentBlock(e.config.BlockSize)); err != nil {
			return err
		}
		e.dropBuffered(e.config.BlockSize)
	}
	return nil
}

func (e *Encoder) enqueueFullBlock(block [][]int64) error {
	if e.config.BlockSplitDepth == 0 {
		if err := e.enqueueBlock(block, e.bufferPTS, nil); err != nil {
			return err
		}
		e.bufferPTS += media.Pts(len(block[0]))
		return nil
	}
	spans, err := chooseBlockSplit(block, e.bitsPerSample, e.config, &e.windows)
	if err != nil {
		return err
	}
	defer releaseBlockSpans(spans)
	for _, span := range spans {
		if err := e.enqueueBlock(blockSlice(block, span.offset, span.length), e.bufferPTS, span.analysis); err != nil {
			return err
		}
		e.bufferPTS += media.Pts(span.length)
	}
	return nil
}

func (e *Encoder) enqueueBlock(block [][]int64, pts media.Pts, analysis *frameAnalysis) error {
	number := e.frameNumber
	if e.config.BlockSplitDepth > 0 {
		number = e.sampleNumber
	}
	var err error
	if analysis != nil {
		_, err = writeAnalyzedFrame(&e.writer, analysis, len(block[0]), e.sampleRate, e.bitsPerSample, number, true, e.config.StreamableSubset)
	} else {
		_, err = encodeFrameWithWriter(block, e.sampleRate, e.bitsPerSample, number, e.config, true, &e.windows, &e.writer)
	}
	if err != nil {
		return err
	}
	pkt := media.NewPacketFromData(e.writer.DetachBytes())
	pkt.MediaType = media.MediaAudio
	pkt.StreamIndex = 0
	pkt.PTS = pts
	pkt.DTS = media.Dts(pts)
	e.pendingQueue = append(e.pendingQueue, pkt)
	e.frameNumber++
	e.sampleNumber += uint64(len(block[0]))
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
	bitsPerSample := e.config.BitsPerSample
	if bitsPerSample <= 0 {
		bitsPerSample = frame.BitsPerSample
	}
	if bitsPerSample <= 0 {
		bitsPerSample = flac.BitDepthFromSampleFormat(format)
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
	bytesPerSample := format.BytesPerSample()
	channels := len(e.buffer)
	plane := frame.Planes()[0]
	minValue := -(int64(1) << uint(e.bitsPerSample-1))
	maxValue := (int64(1) << uint(e.bitsPerSample-1)) - 1
	writeStart := len(e.buffer[0])
	for ch := range e.buffer {
		e.buffer[ch] = e.buffer[ch][:writeStart+frame.Samples]
	}
	for sample := 0; sample < frame.Samples; sample++ {
		for ch := 0; ch < channels; ch++ {
			offset := (sample*channels + ch) * bytesPerSample
			var value int64
			switch format {
			case media.SampleFormatS16:
				value = int64(int16(binary.LittleEndian.Uint16(plane[offset : offset+2])))
			case media.SampleFormatS24:
				raw := int32(uint32(plane[offset]) | uint32(plane[offset+1])<<8 | uint32(plane[offset+2])<<16)
				if raw&0x800000 != 0 {
					raw |= ^int32(0xffffff)
				}
				value = int64(raw)
			case media.SampleFormatS32:
				value = int64(int32(binary.LittleEndian.Uint32(plane[offset : offset+4])))
			default:
				for channel := range e.buffer {
					e.buffer[channel] = e.buffer[channel][:writeStart]
				}
				return fmt.Errorf("unsupported FLAC input format: %s", format)
			}
			if value < minValue || value > maxValue {
				for channel := range e.buffer {
					e.buffer[channel] = e.buffer[channel][:writeStart]
				}
				return fmt.Errorf("FLAC sample %d outside %d-bit range", value, e.bitsPerSample)
			}
			e.buffer[ch][writeStart+sample] = value
		}
	}
	for ch := range e.buffer {
		e.blockView[ch] = e.buffer[ch][writeStart : writeStart+frame.Samples]
	}
	e.md5.Write(e.blockView, e.bitsPerSample)
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
