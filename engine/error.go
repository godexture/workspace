package engine

import "errors"

var (
	ErrEAGAIN = errors.New("resource temporarily unavailable (need more data)")
	ErrEOF    = errors.New("end of file or stream")
)
