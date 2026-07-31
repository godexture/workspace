package node

import "github.com/godexture/core/domain/media"

// Sink consumes decoded frames at the end of a media pipeline.
type Sink interface {
	Node

	InputPorts() map[string]*InPort[media.Frame]
}
