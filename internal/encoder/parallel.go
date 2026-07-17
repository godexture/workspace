package encoder

import (
	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/bits"
)

var outputReady = func() <-chan struct{} {
	ready := make(chan struct{})
	close(ready)
	return ready
}()

// pendingEntry is one slot in Encoder.pendingQueue, in submission order.
// done is nil for entries produced synchronously (workers <= 1); for
// parallel entries it is closed once a worker has filled in packets/err.
type pendingEntry struct {
	packets []*media.Packet
	err     error
	done    chan struct{}
}

// frameJob is one unit of parallel work: either a full config.BlockSize
// block (split == true, subject to config.BlockSplitDepth) or the trailing
// partial block from Flush (split == false, never split, matching the
// sequential path's Flush behavior). frameNumber/sampleBase are assigned by
// the producer before dispatch, since both only depend on how many
// frames/samples were already emitted — not on the (expensive, possibly
// out-of-order) encode itself.
type frameJob struct {
	channels    [][]int64
	pts         media.Pts
	frameNumber uint64
	sampleBase  uint64
	split       bool
	entry       *pendingEntry
}

func (e *Encoder) OutputReady() <-chan struct{} {
	if len(e.pendingQueue) == 0 {
		return nil
	}
	if e.pendingQueue[0].done == nil {
		return outputReady
	}
	return e.pendingQueue[0].done
}

// dispatchFullBlock hands a config.BlockSize-length block to the worker
// pool. It blocks if all workers and the job queue are saturated, which is
// the intended backpressure: SendFrame simply stops accepting more input
// until a worker frees up.
func (e *Encoder) dispatchFullBlock(block [][]int64) {
	entry := &pendingEntry{done: make(chan struct{})}
	e.pendingQueue = append(e.pendingQueue, entry)
	job := frameJob{
		channels:    copyBlock(block),
		pts:         e.bufferPTS,
		frameNumber: e.frameNumber,
		sampleBase:  e.sampleNumber,
		split:       true,
		entry:       entry,
	}
	e.frameNumber++
	e.sampleNumber += uint64(len(block[0]))
	e.jobs <- job
}

// dispatchPartialBlock is Flush's counterpart to dispatchFullBlock: it never
// splits, matching enqueueBlockSync(..., nil) in the sequential path.
func (e *Encoder) dispatchPartialBlock(block [][]int64, pts media.Pts) {
	entry := &pendingEntry{done: make(chan struct{})}
	e.pendingQueue = append(e.pendingQueue, entry)
	job := frameJob{
		channels:    copyBlock(block),
		pts:         pts,
		frameNumber: e.frameNumber,
		sampleBase:  e.sampleNumber,
		split:       false,
		entry:       entry,
	}
	e.frameNumber++
	e.sampleNumber += uint64(len(block[0]))
	e.jobs <- job
}

func copyBlock(block [][]int64) [][]int64 {
	buf := make([][]int64, len(block))
	for ch := range block {
		buf[ch] = append([]int64(nil), block[ch]...)
	}
	return buf
}

// runWorker owns its bits.Writer and windowSet for its whole lifetime, so
// concurrent workers never contend on encoder state; e.sampleRate,
// e.bitsPerSample and e.config are read-only by the time any job is
// dispatched (set once during the first SendFrame, before workers start
// receiving jobs), and the channel send/receive in dispatch*/runWorker
// establishes the happens-before relationship for that initialization.
func (e *Encoder) runWorker() {
	writer := &bits.Writer{}
	windows := newWindowSet(e.config.Apodizations)
	for job := range e.jobs {
		packets, err := e.encodeJob(job, writer, &windows)
		job.entry.packets = packets
		job.entry.err = err
		close(job.entry.done)
	}
}

// encodeJob mirrors enqueueFullBlock+enqueueBlockSync's logic, but as a pure
// function of its job/writer/windows arguments instead of encoder state, so
// it is safe to call from multiple worker goroutines concurrently.
func (e *Encoder) encodeJob(job frameJob, writer *bits.Writer, windows *windowSet) ([]*media.Packet, error) {
	if job.split && e.config.BlockSplitDepth > 0 {
		spans, err := chooseBlockSplit(job.channels, e.bitsPerSample, e.config, windows)
		if err != nil {
			return nil, err
		}
		defer releaseBlockSpans(spans)
		packets := make([]*media.Packet, 0, len(spans))
		pts := job.pts
		sampleNumber := job.sampleBase
		for _, span := range spans {
			// BlockSplitEstimated leaves span.analysis nil (only the cheap
			// estimate was computed while choosing boundaries); encode it
			// the same way enqueueBlockSync's analysis==nil branch does.
			var err error
			if span.analysis != nil {
				_, err = writeAnalyzedFrame(writer, span.analysis, span.length, e.sampleRate, e.bitsPerSample, sampleNumber, true, e.config.StreamableSubset)
			} else {
				spanBlock := blockSlice(job.channels, span.offset, span.length)
				_, err = encodeFrameWithWriter(spanBlock, e.sampleRate, e.bitsPerSample, sampleNumber, e.config, true, windows, writer)
			}
			if err != nil {
				return nil, err
			}
			packets = append(packets, newFramePacket(writer.DetachBytes(), pts))
			pts += media.Pts(span.length)
			sampleNumber += uint64(span.length)
		}
		return packets, nil
	}

	number := job.frameNumber
	if e.config.BlockSplitDepth > 0 {
		number = job.sampleBase
	}
	if _, err := encodeFrameWithWriter(job.channels, e.sampleRate, e.bitsPerSample, number, e.config, true, windows, writer); err != nil {
		return nil, err
	}
	return []*media.Packet{newFramePacket(writer.DetachBytes(), job.pts)}, nil
}
