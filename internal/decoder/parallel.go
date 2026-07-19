package decoder

import (
	"fmt"
	"sync"

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
	md5    []byte
	err    error
	done   chan struct{}
}

type frameJob struct {
	data   []byte
	info   streaminfo.StreamInfo
	strict bool
	entry  *pendingEntry
}

var sharedDecoderPool struct {
	once sync.Once
	jobs chan frameJob
}

func decoderJobs(workers int) chan frameJob {
	sharedDecoderPool.once.Do(func() {
		sharedDecoderPool.jobs = make(chan frameJob, 2*workers)
		for range workers {
			go runDecoderWorker(sharedDecoderPool.jobs)
		}
	})
	return sharedDecoderPool.jobs
}

func runDecoderWorker(jobs <-chan frameJob) {
	var workspace decodeWorkspace
	for job := range jobs {
		decoded, err := decodeFrame(job.data, job.info, job.strict, &workspace)
		if err == nil && decoded.Bytes != len(job.data) {
			err = fmt.Errorf("FLAC packet contains trailing data: decoded %d of %d bytes", decoded.Bytes, len(job.data))
		}
		if err == nil {
			job.entry.header = decoded.Header
			job.entry.audio, err = buildAudioFrame(decoded)
			if err == nil && job.strict && job.entry.audio.Format.BytesPerSample() != (decoded.Header.BitsPerSample+7)/8 {
				job.entry.md5 = flac.PackPCMMD5(nil, decoded.Samples, decoded.Header.BitsPerSample)
			}
		}
		job.entry.err = err
		close(job.entry.done)
	}
}

func (d *Decoder) OutputReady() <-chan struct{} {
	if len(d.pendingQueue) > 0 {
		return d.pendingQueue[0].done
	}
	if d.flushed {
		return decoderOutputReady
	}
	return nil
}
