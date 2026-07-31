package decoder

import (
	"fmt"

	"github.com/godexture/codec-flac/internal/flac"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/format-flac/frame"
	"github.com/godexture/format-flac/streaminfo"
	"github.com/godexture/sdk/pool"
)

var decoderOutputReady = func() <-chan struct{} {
	ready := make(chan struct{})
	close(ready)
	return ready
}()

// pendingEntry's completion is tracked with a plain bool (ready) instead of
// a per-entry channel: every entry that ever needs to block a waiter shares
// the same Decoder-level waitCh (see markReady/waitForEntry), so completion
// signaling costs one channel per contended wait instead of one per packet.
type pendingEntry struct {
	header frame.Header
	audio  *media.AudioFrame
	pts    media.Pts
	md5    []byte
	err    error
	ready  bool
}

type frameJob struct {
	d      *Decoder
	data   []byte
	// dataBuf is the pool.Get-backed storage for data, non-nil for jobs
	// dispatched to the worker pool (see SendPacket). It's returned to the
	// pool once decodeJob is done reading it, in runJob. nil for jobs run
	// synchronously in SendPacket, which read pkt.Data() directly and never
	// need their own copy.
	dataBuf *[]byte
	pts     media.Pts
	info    streaminfo.StreamInfo
	strict  bool
	entry   *pendingEntry
}

// Run lets frameJob be submitted to a WorkerPool directly (see
// registry.Task), avoiding the extra closure allocation a func() wrapper
// would need on top of the job struct that must already be heap-allocated to
// outlive SendPacket.
func (job *frameJob) Run() {
	job.d.runJob(*job)
}

// runJob runs on a shared pool worker, not one dedicated to this decoder, so
// it borrows a scratch decodeWorkspace for the duration of the call instead
// of owning one for a whole goroutine's lifetime.
func (d *Decoder) runJob(job frameJob) {
	workspace := d.acquireWorkspace()
	decodeJob(job, workspace)
	d.releaseWorkspace(workspace)
	if job.dataBuf != nil {
		pool.Put(job.dataBuf)
	}
	d.markReady(job.entry)
}

func (d *Decoder) acquireWorkspace() *decodeWorkspace {
	return d.scratch.Get()
}

func (d *Decoder) releaseWorkspace(workspace *decodeWorkspace) {
	d.scratch.Put(workspace)
}

func decodeJob(job frameJob, workspace *decodeWorkspace) {
	decoded, err := decodeFrame(job.data, job.info, job.strict, workspace)
	if err == nil && decoded.Bytes != len(job.data) {
		err = fmt.Errorf("FLAC packet contains trailing data: decoded %d of %d bytes", decoded.Bytes, len(job.data))
	}
	if err == nil {
		job.entry.header = decoded.Header
		job.entry.audio, err = buildAudioFrame(decoded, job.pts)
		if err == nil && job.strict && job.entry.audio.Format.BytesPerSample() != (decoded.Header.BitsPerSample+7)/8 {
			job.entry.md5 = flac.PackPCMMD5(nil, decoded.Samples, decoded.Header.BitsPerSample)
		}
	}
	job.entry.err = err
}

// markReady marks entry as complete and wakes anyone currently blocked in
// OutputReady or waitForEntry. Safe to call from a pool worker goroutine
// (that's its only caller: runJob, after decodeJob finishes on the worker).
func (d *Decoder) markReady(entry *pendingEntry) {
	d.gate.MarkReady(func() { entry.ready = true })
}

// waitForEntry blocks until entry.ready, regardless of whether entry is
// still the queue head. Used by Close, which must wait out every pending
// entry's task before releasing it, not just the head's.
func (d *Decoder) waitForEntry(entry *pendingEntry) {
	d.gate.Wait(func() bool { return entry.ready })
}

func (d *Decoder) OutputReady() <-chan struct{} {
	d.gate.Lock()
	defer d.gate.Unlock()
	if len(d.pendingQueue) > 0 {
		if d.pendingQueue[0].ready {
			return decoderOutputReady
		}
		return d.gate.ChanLocked()
	}
	if d.flushed {
		return decoderOutputReady
	}
	return nil
}
