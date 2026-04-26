package node

import (
	"context"

	"github.com/godexture/core/domain/media"
)

type Filter interface {
	Node

	Process(ctx context.Context) error

	InputPorts() map[string]InPort[media.Frame]
	OutputPorts() map[string]OutPort[media.Frame]
}
