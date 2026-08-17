package file

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// ioBoundaryError is the only error shape that leaves a filesystem operation
// in this provider. The standard library's PathError and LinkError retain the
// resolver path in Error, formatting, and their exported fields. Keeping them
// as a private cause preserves errors.Is through Is while making every public
// rendering and errors.As traversal contain only stable operation and class
// information.
type ioBoundaryError struct {
	operation string
	scheme    string
	class     string
	cause     error
}

func (e *ioBoundaryError) Error() string {
	if e == nil {
		return "file I/O failed"
	}
	scheme := e.scheme
	if scheme == "" {
		scheme = "file"
	}
	return fmt.Sprintf("%s %s failed (%s)", scheme, e.operation, e.class)
}

func (e *ioBoundaryError) Is(target error) bool {
	if e == nil {
		return false
	}
	return errors.Is(e.cause, target)
}

// Format deliberately ignores the wrapped PathError for every fmt verb,
// including %#v and %+v.
func (e *ioBoundaryError) Format(state fmt.State, verb rune) {
	if verb == 'q' {
		_, _ = fmt.Fprintf(state, "%q", e.Error())
		return
	}
	_, _ = io.WriteString(state, e.Error())
}

func redactIO(operation string, err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*ioBoundaryError); ok {
		return err
	}
	var pathErr *os.PathError
	var linkErr *os.LinkError
	if !errors.As(err, &pathErr) && !errors.As(err, &linkErr) {
		// EOF, context cancellation, and ordinary provider errors carry no
		// filesystem name and retain their exact identity for callers.
		return err
	}
	return &ioBoundaryError{operation: operation, scheme: "file", class: ioErrorClass(err), cause: err}
}

func ioErrorClass(err error) string {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "not-found"
	case errors.Is(err, os.ErrPermission):
		return "permission"
	case errors.Is(err, os.ErrExist):
		return "already-exists"
	case errors.Is(err, os.ErrInvalid):
		return "invalid"
	case errors.Is(err, io.EOF):
		return "eof"
	case errors.Is(err, io.ErrUnexpectedEOF):
		return "unexpected-eof"
	default:
		return "io"
	}
}
