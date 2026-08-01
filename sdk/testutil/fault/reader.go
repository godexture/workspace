package fault

import "io"

// Reader wraps an io.ReadSeeker that fails with err once at least
// threshold bytes have been read across all Read calls, or as soon as
// Fail is called. Seek always passes through to the underlying
// ReadSeeker: tests that need a failing Seek should use a ReadSeeker whose
// own Seek fails, since demux/decode Seek failures are format-specific.
type Reader struct {
	r         io.ReadSeeker
	err       error
	read      int
	threshold int
	forced    bool
}

// NewReader returns a Reader that fails with err starting at the
// threshold'th byte read.
func NewReader(r io.ReadSeeker, threshold int, err error) *Reader {
	return &Reader{r: r, threshold: threshold, err: err}
}

// Fail makes every subsequent Read call fail immediately, regardless of
// the configured byte threshold.
func (r *Reader) Fail() { r.forced = true }

func (r *Reader) Read(p []byte) (int, error) {
	if r.forced || r.read >= r.threshold {
		return 0, r.err
	}
	if remaining := r.threshold - r.read; len(p) > remaining {
		p = p[:remaining]
	}
	n, err := r.r.Read(p)
	r.read += n
	return n, err
}

func (r *Reader) Seek(offset int64, whence int) (int64, error) {
	return r.r.Seek(offset, whence)
}
