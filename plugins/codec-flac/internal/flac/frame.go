package flac

import "github.com/godexture/godec/plugins/format-flac/frame"

// Frame represents a fully decoded or to-be-encoded FLAC frame.
type Frame struct {
	Header  frame.Header
	Samples [][]int64
	Bytes   int
}
