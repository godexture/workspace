// Package fault provides small io.Reader/io.Writer wrappers that inject a
// caller-controlled failure, for testing that format/codec lifecycle
// methods (demux read, mux Add/SetMetadata/WriteHeader/WritePacket/
// WriteTrailer, ...) surface and clean up after I/O failures correctly
// instead of only being exercised against always-succeeding in-memory
// buffers.
package fault

import "io"

// Writer wraps an io.Writer that fails with err once at least threshold
// bytes have been requested across all Write calls, or as soon as Fail is
// called, whichever comes first. A call that straddles the threshold
// writes its allowed prefix through to the underlying writer (matching a
// real short write) and reports err for that same call.
type Writer struct {
	w         io.Writer
	err       error
	written   int
	threshold int
	forced    bool
}

// NewWriter returns a Writer that fails with err starting at the
// threshold'th byte. Use a threshold of 0 to fail on the first Write, or
// call Fail later to fail from an arbitrary point in the call sequence
// instead of a byte offset.
func NewWriter(w io.Writer, threshold int, err error) *Writer {
	return &Writer{w: w, threshold: threshold, err: err}
}

// Fail makes every subsequent Write call fail immediately, regardless of
// the configured byte threshold. It lets a test let one muxer phase (e.g.
// AddStream/WriteHeader) succeed and then fail a later phase (e.g.
// WritePacket/WriteTrailer) precisely.
func (w *Writer) Fail() { w.forced = true }

func (w *Writer) Write(p []byte) (int, error) {
	if w.forced || w.written >= w.threshold {
		return 0, w.err
	}
	allowed := w.threshold - w.written
	if len(p) <= allowed {
		n, err := w.w.Write(p)
		w.written += n
		return n, err
	}
	n, err := w.w.Write(p[:allowed])
	w.written += n
	if err != nil {
		return n, err
	}
	return n, w.err
}

// SeekWriter wraps an io.WriteSeeker, injecting Write failures the same
// way as Writer while passing Seek straight through. Muxers that
// type-assert their output for io.Seeker to choose a seekable/
// non-seekable code path need this instead of Writer, whose plain
// io.Writer shape would otherwise silently force the non-seekable path.
type SeekWriter struct {
	*Writer
	seeker io.Seeker
}

// NewSeekWriter returns a SeekWriter that fails with err starting at the
// threshold'th byte, matching NewWriter.
func NewSeekWriter(w io.WriteSeeker, threshold int, err error) *SeekWriter {
	return &SeekWriter{Writer: NewWriter(w, threshold, err), seeker: w}
}

func (w *SeekWriter) Seek(offset int64, whence int) (int64, error) {
	return w.seeker.Seek(offset, whence)
}
