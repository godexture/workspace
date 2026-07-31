package node

import "github.com/godexture/godec/core/domain/media"

// Sink consumes decoded frames at the end of a media pipeline.
type Sink interface {
	Node

	InputPorts() map[string]*InPort[media.Frame]
}
