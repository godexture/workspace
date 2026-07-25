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
// ready is true immediately for entries produced synchronously (pool ==
// nil); for parallel entries it's set once a pool task has filled in
// packets/err (see markReady/waitForEntry: every entry that needs to block a
// waiter shares the Encoder's single lazily-created waitCh rather than
// owning its own channel).
type pendingEntry struct {
	packets []*media.Packet
	err     error
	ready   bool
}

// frameJob is one unit of parallel work: either a full config.BlockSize
// block (split == true, subject to config.BlockSplitDepth) or the trailing
// partial block from Flush (split == false, never split, matching the
// sequential path's Flush behavior). frameNumber/sampleBase are assigned by
// the producer before dispatch, since both only depend on how many
// frames/samples were already emitted — not on the (expensive, possibly
// out-of-order) encode itself.
type frameJob struct {
	e        *Encoder
	channels [][]int64
	// blockCopy is channels' pooled backing storage (see acquireBlockCopy),
	// returned to e.blockPool in runJob once encodeJob is done reading it.
	blockCopy   *blockCopy
	pts         media.Pts
	frameNumber uint64
	sampleBase  uint64
	split       bool
	entry       *pendingEntry
}

// Run lets frameJob be submitted to a WorkerPool directly (see
// registry.Task), avoiding the extra closure allocation a func() wrapper
// would need on top of the job struct that must already be heap-allocated to
// outlive dispatchFullBlock/dispatchPartialBlock.
func (job *frameJob) Run() {
	job.e.runJob(*job)
}

// encodeScratch is the per-task working state a submitted job needs. It is
// pooled per Encoder (via Encoder.scratch) so repeated blocks from the same
// encoder reuse a writer/windowSet instead of allocating fresh ones on every
// pool task, even though tasks no longer run on a goroutine dedicated to this
// encoder.
type encodeScratch struct {
	writer  bits.Writer
	windows windowSet
}

func (e *Encoder) acquireScratch() *encodeScratch {
	return e.scratch.Get()
}

func (e *Encoder) releaseScratch(s *encodeScratch) {
	e.scratch.Put(s)
}

// markReady marks entry as complete and wakes anyone currently blocked in
// OutputReady or waitForEntry. Safe to call from a pool worker goroutine
// (that's its only caller: runJob, after encodeJob finishes on the worker).
func (e *Encoder) markReady(entry *pendingEntry) {
	e.gate.MarkReady(func() { entry.ready = true })
}

// waitForEntry blocks until entry.ready, regardless of whether entry is
// still the queue head. Used by ReceivePacket (see its docs for why it must
// always wait out the head rather than returning ErrEAGAIN) and Close.
func (e *Encoder) waitForEntry(entry *pendingEntry) {
	e.gate.Wait(func() bool { return entry.ready })
}

func (e *Encoder) OutputReady() <-chan struct{} {
	e.gate.Lock()
	defer e.gate.Unlock()
	if len(e.pendingQueue) == 0 {
		return nil
	}
	if e.pendingQueue[0].ready {
		return outputReady
	}
	return e.gate.ChanLocked()
}

// dispatchFullBlock hands a config.BlockSize-length block to the shared
// worker pool. Submit blocks if the pool's queue and workers are all
// saturated, which is the intended backpressure: SendFrame simply stops
// accepting more input until a worker frees up.
func (e *Encoder) dispatchFullBlock(block [][]int64) {
	entry := &pendingEntry{}
	e.pendingQueue = append(e.pendingQueue, entry)
	bc := e.acquireBlockCopy(block)
	job := &frameJob{
		e:           e,
		channels:    bc.channels,
		blockCopy:   bc,
		pts:         e.bufferPTS,
		frameNumber: e.frameNumber,
		sampleBase:  e.sampleNumber,
		split:       true,
		entry:       entry,
	}
	e.frameNumber++
	e.sampleNumber += uint64(len(block[0]))
	e.pool.Submit(job)
}

// dispatchPartialBlock is Flush's counterpart to dispatchFullBlock: it never
// splits, matching enqueueBlockSync(..., nil) in the sequential path.
func (e *Encoder) dispatchPartialBlock(block [][]int64, pts media.Pts) {
	entry := &pendingEntry{}
	e.pendingQueue = append(e.pendingQueue, entry)
	bc := e.acquireBlockCopy(block)
	job := &frameJob{
		e:           e,
		channels:    bc.channels,
		blockCopy:   bc,
		pts:         pts,
		frameNumber: e.frameNumber,
		sampleBase:  e.sampleNumber,
		split:       false,
		entry:       entry,
	}
	e.frameNumber++
	e.sampleNumber += uint64(len(block[0]))
	e.pool.Submit(job)
}

// blockCopy is a job's private, pool-recycled copy of the block it was
// dispatched with (see acquireBlockCopy): the encoder's own e.buffer keeps
// being written to and reused as soon as dispatch returns, so async jobs
// need an independent copy rather than a view into it.
type blockCopy struct {
	channels [][]int64
}

// acquireBlockCopy returns a blockCopy holding an independent copy of
// block's samples, reusing a pooled one (grown as needed) instead of
// allocating fresh channel slices on every dispatch.
func (e *Encoder) acquireBlockCopy(block [][]int64) *blockCopy {
	bc := e.blockPool.Get()
	if cap(bc.channels) < len(block) {
		grown := make([][]int64, len(block))
		copy(grown, bc.channels)
		bc.channels = grown
	} else {
		bc.channels = bc.channels[:len(block)]
	}
	for ch := range block {
		dst := bc.channels[ch]
		if cap(dst) < len(block[ch]) {
			dst = make([]int64, len(block[ch]))
		} else {
			dst = dst[:len(block[ch])]
		}
		copy(dst, block[ch])
		bc.channels[ch] = dst
	}
	return bc
}

func (e *Encoder) releaseBlockCopy(bc *blockCopy) {
	e.blockPool.Put(bc)
}

// runJob runs on a shared pool worker, not one dedicated to this encoder, so
// it borrows scratch state for the duration of the call instead of owning it
// for a whole goroutine's lifetime.
func (e *Encoder) runJob(job frameJob) {
	scratch := e.acquireScratch()
	packets, err := e.encodeJob(job, &scratch.writer, &scratch.windows)
	e.releaseScratch(scratch)
	e.releaseBlockCopy(job.blockCopy)
	job.entry.packets = packets
	job.entry.err = err
	e.markReady(job.entry)
}

// encodeJob mirrors enqueueFullBlock+enqueueBlockSync's logic, but as a pure
// function of its job/writer/windows arguments instead of encoder state, so
// it is safe to call from multiple pool workers concurrently.
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
				releasePackets(packets)
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

func releasePackets(packets []*media.Packet) {
	for _, packet := range packets {
		if packet != nil {
			packet.Release()
		}
	}
}
