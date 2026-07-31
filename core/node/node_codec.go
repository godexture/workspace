package node

import "github.com/godexture/godec/core/domain/media"

type Encoder interface {
	Node

	InputPorts() map[string]*InPort[media.Frame]
	OutputPorts() map[string]*OutPort[*media.Packet]
}

type Decoder interface {
	Node

	InputPorts() map[string]*InPort[*media.Packet]
	OutputPorts() map[string]*OutPort[media.Frame]
}
