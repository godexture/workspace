package decoder

import (
	"fmt"

	"github.com/godexture/codec-flac/internal/flac"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/format-flac/frame"
	"github.com/godexture/format-flac/streaminfo"
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
	pts    media.Pts
	info   streaminfo.StreamInfo
	strict bool
	entry  *pendingEntry
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
	d.markReady(job.entry)
}

func (d *Decoder) acquireWorkspace() *decodeWorkspace {
	if v := d.scratch.Get(); v != nil {
		return v.(*decodeWorkspace)
	}
	return &decodeWorkspace{}
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
	d.mu.Lock()
	entry.ready = true
	if d.waitCh != nil {
		close(d.waitCh)
		d.waitCh = nil
	}
	d.mu.Unlock()
}

// waitChanLocked returns the channel that closes the next time any pending
// entry completes, creating it on first use. d.mu must be held.
func (d *Decoder) waitChanLocked() <-chan struct{} {
	if d.waitCh == nil {
		d.waitCh = make(chan struct{})
	}
	return d.waitCh
}

// waitForEntry blocks until entry.ready, regardless of whether entry is
// still the queue head. Used by Close, which must wait out every pending
// entry's task before releasing it, not just the head's.
func (d *Decoder) waitForEntry(entry *pendingEntry) {
	for {
		d.mu.Lock()
		if entry.ready {
			d.mu.Unlock()
			return
		}
		ch := d.waitChanLocked()
		d.mu.Unlock()
		<-ch
	}
}

func (d *Decoder) OutputReady() <-chan struct{} {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.pendingQueue) > 0 {
		if d.pendingQueue[0].ready {
			return decoderOutputReady
		}
		return d.waitChanLocked()
	}
	if d.flushed {
		return decoderOutputReady
	}
	return nil
}
