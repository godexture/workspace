package media

import "github.com/godexture/core/domain/metadata"

type Frame interface {
	Retainer
	Pts() Pts
	Metadata() *metadata.Bundle
}
