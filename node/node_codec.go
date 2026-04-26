package node

import "github.com/godexture/core/domain/media"

type Encoder interface {
	Node

	Encode(media.Frame) (*media.Packet, error)

	InputPorts() map[string]InPort[media.Frame]
	OutputPorts() map[string]OutPort[*media.Packet]
}

type Decoder interface {
	Node

	Decode(*media.Packet) (media.Frame, error)

	InputPorts() map[string]InPort[*media.Packet]
	OutputPorts() map[string]OutPort[media.Frame]
}
