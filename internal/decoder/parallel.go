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

type pendingEntry struct {
	header frame.Header
	audio  *media.AudioFrame
	pts    media.Pts
	md5    []byte
	err    error
	done   chan struct{}
}

type frameJob struct {
	data   []byte
	pts    media.Pts
	info   streaminfo.StreamInfo
	strict bool
	entry  *pendingEntry
}

// runJob runs on a shared pool worker, not one dedicated to this decoder, so
// it borrows a scratch decodeWorkspace for the duration of the call instead
// of owning one for a whole goroutine's lifetime.
func (d *Decoder) runJob(job frameJob) {
	workspace := d.acquireWorkspace()
	decodeJob(job, workspace)
	d.releaseWorkspace(workspace)
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
	if job.entry.done != nil {
		close(job.entry.done)
	}
}

func (d *Decoder) OutputReady() <-chan struct{} {
	if len(d.pendingQueue) > 0 {
		if d.pendingQueue[0].done == nil {
			return decoderOutputReady
		}
		return d.pendingQueue[0].done
	}
	if d.flushed {
		return decoderOutputReady
	}
	return nil
}
