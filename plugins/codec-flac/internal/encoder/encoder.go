package encoder

import (
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"

	"github.com/godexture/codec-flac/internal/flac"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/format-flac/streaminfo"
	"github.com/godexture/sdk/bits"
	"github.com/godexture/sdk/engine"
)

type Encoder struct {
	config flac.EncoderConfig

	pendingQueue []*pendingEntry
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
	md5           *flac.PCMMD5

	// Sequential path (workers <= 1): reused across the whole stream, no
	// goroutines or channels involved. This is the pre-parallel code path,
	// kept byte-for-byte so Workers=1 stays free of any parallel overhead.
	windows windowSet
	writer  bits.Writer

	// Parallel path (workers > 1): full (and, when flushing, the trailing
	// partial) blocks are dispatched to a fixed worker pool over jobs. Each
	// worker owns its own bits.Writer/windowSet so there is no contention.
	// Workers never changes the encoded bytes (see encoder_test.go's
	// Workers=1-vs-N equivalence test) — only how the work is scheduled.
	workers int
	jobs    chan frameJob
}

func NewEncoder(stream media.StreamInfo, cfg flac.EncoderConfig) (*Encoder, error) {
	cfg = flac.MergeEncoderConfigForFactory(cfg, stream)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	workers := cfg.Workers
	if workers == 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	e := &Encoder{config: cfg, windows: newWindowSet(cfg.Apodizations), md5: flac.NewPCMMD5(), workers: workers}
	fmt.Printf("FLAC encoder: %d workers\n", workers)
	if workers > 1 {
		e.jobs = make(chan frameJob, 2*workers)
		for i := 0; i < workers; i++ {
			go e.runWorker()
		}
	}
	return e, nil
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

// ReceivePacket drains pendingQueue in submission order. Entries produced by
// the parallel path may still be in flight (entry.done != nil), in which
// case this blocks on that specific entry until a worker finishes it.
//
// It deliberately never returns ErrEAGAIN just because the head isn't ready
// yet: callers may wire this encoder into a pipeline with fan-out (e.g. a
// tee feeding both this encoder and a comparison against its own output),
// where upstream progress depends on the encoder actually producing a
// packet rather than the caller pulling more input first. Any policy that
// withholds a dispatched-but-pending packet in favor of "let more work
// queue up" can deadlock such a graph — see the regression that motivated
// this comment. This does mean input chunked smaller than BlockSize (one
// job dispatched per full block) gets pipelined roughly one job deep rather
// than Workers deep; blocks dispatched together (e.g. a decoder frame
// larger than BlockSize) still encode in parallel within a single
// emitFullBlocks call.
func (e *Encoder) ReceivePacket() (*media.Packet, error) {
	for len(e.pendingQueue) > 0 {
		head := e.pendingQueue[0]
		if head.done != nil {
			<-head.done
			head.done = nil
		}
		if head.err != nil {
			e.popPendingEntry()
			return nil, head.err
		}
		if len(head.packets) == 0 {
			e.popPendingEntry()
			continue
		}
		pkt := head.packets[0]
		head.packets[0] = nil
		head.packets = head.packets[1:]
		if len(head.packets) == 0 {
			e.popPendingEntry()
		}
		return pkt, nil
	}
	if e.flushed {
		return nil, engine.ErrEOF
	}
	return nil, engine.ErrEAGAIN
}

// popPendingEntry removes the drained head entry, resetting pendingQueue to
// nil (not just an empty slice) once it empties out, matching the
// pre-parallel contract callers rely on.
func (e *Encoder) popPendingEntry() {
	e.pendingQueue = e.pendingQueue[1:]
	if len(e.pendingQueue) == 0 {
		e.pendingQueue = nil
	}
}

func (e *Encoder) Flush() error {
	if e.flushed {
		return nil
	}
	if e.buffered > 0 {
		block := e.currentBlock(e.buffered)
		if e.workers > 1 {
			e.dispatchPartialBlock(block, e.bufferPTS)
		} else if err := e.enqueueBlockSync(block, e.bufferPTS, nil); err != nil {
			return err
		}
		e.dropBuffered(e.buffered)
	}
	e.flushed = true
	if e.jobs != nil {
		close(e.jobs)
	}
	sum := e.MD5()
	e.pendingQueue = append(e.pendingQueue, &pendingEntry{packets: []*media.Packet{
		media.NewPacketEvent(media.PacketKindStreamEnd, 0, []media.CodecParameters{
			media.NewCodecParameters[streaminfo.PCMMD5Parameters](sum[:]),
		}),
	}})
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
	if e.workers > 1 {
		e.dispatchFullBlock(block)
		e.bufferPTS += media.Pts(len(block[0]))
		return nil
	}
	if e.config.BlockSplitDepth == 0 {
		if err := e.enqueueBlockSync(block, e.bufferPTS, nil); err != nil {
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
		if err := e.enqueueBlockSync(blockSlice(block, span.offset, span.length), e.bufferPTS, span.analysis); err != nil {
			return err
		}
		e.bufferPTS += media.Pts(span.length)
	}
	return nil
}

// enqueueBlockSync is the sequential (workers <= 1) code path: it encodes
// directly using the Encoder's own writer/windows and appends an
// already-complete pendingEntry, matching the pre-parallel behavior exactly.
func (e *Encoder) enqueueBlockSync(block [][]int64, pts media.Pts, analysis *frameAnalysis) error {
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
	pkt := newFramePacket(e.writer.DetachBytes(), pts)
	e.pendingQueue = append(e.pendingQueue, &pendingEntry{packets: []*media.Packet{pkt}})
	e.frameNumber++
	e.sampleNumber += uint64(len(block[0]))
	return nil
}

func newFramePacket(data []byte, pts media.Pts) *media.Packet {
	pkt := media.NewPacketFromData(data)
	pkt.MediaType = media.MediaAudio
	pkt.StreamIndex = 0
	pkt.PTS = pts
	pkt.DTS = media.Dts(pts)
	return pkt
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
	e.md5.Write(e.blockView, e.bitsPerSample)
	e.buffered += frame.Samples
	return nil
}

func deinterleaveS16(buffer [][]int64, plane []byte, writeStart, samples, channels int, minValue, maxValue int64, bitsPerSample int) error {
	for sample := 0; sample < samples; sample++ {
		for ch := 0; ch < channels; ch++ {
			offset := (sample*channels + ch) * 2
			value := int64(int16(binary.LittleEndian.Uint16(plane[offset : offset+2])))
			if value < minValue || value > maxValue {
				return fmt.Errorf("FLAC sample %d outside %d-bit range", value, bitsPerSample)
			}
			buffer[ch][writeStart+sample] = value
		}
	}
	return nil
}

func deinterleaveS24(buffer [][]int64, plane []byte, writeStart, samples, channels int, minValue, maxValue int64, bitsPerSample int) error {
	for sample := 0; sample < samples; sample++ {
		for ch := 0; ch < channels; ch++ {
			offset := (sample*channels + ch) * 3
			raw := int32(uint32(plane[offset]) | uint32(plane[offset+1])<<8 | uint32(plane[offset+2])<<16)
			if raw&0x800000 != 0 {
				raw |= ^int32(0xffffff)
			}
			value := int64(raw)
			if value < minValue || value > maxValue {
				return fmt.Errorf("FLAC sample %d outside %d-bit range", value, bitsPerSample)
			}
			buffer[ch][writeStart+sample] = value
		}
	}
	return nil
}

func deinterleaveS32(buffer [][]int64, plane []byte, writeStart, samples, channels int, minValue, maxValue int64, bitsPerSample int) error {
	for sample := 0; sample < samples; sample++ {
		for ch := 0; ch < channels; ch++ {
			offset := (sample*channels + ch) * 4
			value := int64(int32(binary.LittleEndian.Uint32(plane[offset : offset+4])))
			if value < minValue || value > maxValue {
				return fmt.Errorf("FLAC sample %d outside %d-bit range", value, bitsPerSample)
			}
			buffer[ch][writeStart+sample] = value
		}
	}
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
