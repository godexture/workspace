package format

import (
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/schema"
)

type chunkSchemaID struct{}

var chunkSchema = schema.Define[chunkSchemaID](schema.Traits[packet.Chunk]{
	Fork: func(value packet.Chunk) packet.Chunk { return value.Share() },
	Drop: func(value packet.Chunk) { value.Release() },
	Size: func(value packet.Chunk) int { return len(value.Bytes()) },
	Time: func(value packet.Chunk) (int64, bool) {
		pts, ok := value.PTS().Get()
		return pts.Int64(), ok
	},
})

// Chunks is the canonical container-framing schema shared by formats and parsers.
func Chunks() schema.Type[packet.Chunk] { return chunkSchema }
