package mp3

import "errors"

var (
	ErrMalformed    = errors.New("malformed MP3 stream")
	ErrUnsupported  = errors.New("unsupported MP3 stream")
	ErrTruncated    = errors.New("MP3 stream is truncated")
	ErrPartialFrame = errors.New("MP3 stream ends inside a frame")
)
