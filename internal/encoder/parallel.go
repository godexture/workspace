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
	if v := e.scratch.Get(); v != nil {
		return v.(*encodeScratch)
	}
	return &encodeScratch{windows: newWindowSet(e.config.Apodizations)}
}

func (e *Encoder) releaseScratch(s *encodeScratch) {
	e.scratch.Put(s)
}

// markReady marks entry as complete and wakes anyone currently blocked in
// OutputReady or waitForEntry. Safe to call from a pool worker goroutine
// (that's its only caller: runJob, after encodeJob finishes on the worker).
func (e *Encoder) markReady(entry *pendingEntry) {
	e.mu.Lock()
	entry.ready = true
	if e.waitCh != nil {
		close(e.waitCh)
		e.waitCh = nil
	}
	e.mu.Unlock()
}

// waitChanLocked returns the channel that closes the next time any pending
// entry completes, creating it on first use. e.mu must be held.
func (e *Encoder) waitChanLocked() <-chan struct{} {
	if e.waitCh == nil {
		e.waitCh = make(chan struct{})
	}
	return e.waitCh
}

// waitForEntry blocks until entry.ready, regardless of whether entry is
// still the queue head. Used by ReceivePacket (see its docs for why it must
// always wait out the head rather than returning ErrEAGAIN) and Close.
func (e *Encoder) waitForEntry(entry *pendingEntry) {
	for {
		e.mu.Lock()
		if entry.ready {
			e.mu.Unlock()
			return
		}
		ch := e.waitChanLocked()
		e.mu.Unlock()
		<-ch
	}
}

func (e *Encoder) OutputReady() <-chan struct{} {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.pendingQueue) == 0 {
		return nil
	}
	if e.pendingQueue[0].ready {
		return outputReady
	}
	return e.waitChanLocked()
}

// dispatchFullBlock hands a config.BlockSize-length block to the shared
// worker pool. Submit blocks if the pool's queue and workers are all
// saturated, which is the intended backpressure: SendFrame simply stops
// accepting more input until a worker frees up.
func (e *Encoder) dispatchFullBlock(block [][]int64) {
	entry := &pendingEntry{}
	e.mu.Lock()
	e.pendingQueue = append(e.pendingQueue, entry)
	e.mu.Unlock()
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
	e.mu.Lock()
	e.pendingQueue = append(e.pendingQueue, entry)
	e.mu.Unlock()
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
