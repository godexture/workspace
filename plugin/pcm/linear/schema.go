package linear

import (
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/schema"
)

type (
	bytesSchemaID  struct{}
	chunkSchemaID  struct{}
	packetSchemaID struct{}
)

var (
	bytesSchema = schema.Define[bytesSchemaID](schema.Traits[[]byte]{
		Fork: func(value []byte) []byte { return append([]byte(nil), value...) },
		Size: func(value []byte) int { return len(value) },
	})
	chunkSchema = schema.Define[chunkSchemaID](schema.Traits[packet.Chunk]{
		Fork: func(value packet.Chunk) packet.Chunk { return value.Share() },
		Drop: func(value packet.Chunk) { value.Release() },
		Size: func(value packet.Chunk) int { return len(value.Bytes()) },
	})
	packetSchema = schema.Define[packetSchemaID](schema.Traits[packet.Packet]{
		Fork: func(value packet.Packet) packet.Packet { return value.Share() },
		Drop: func(value packet.Packet) { value.Release() },
		Size: func(value packet.Packet) int { return len(value.Bytes()) },
	})
)

// Bytes is the raw linear-PCM byte stream schema used at Access boundaries.
func Bytes() schema.Type[[]byte] { return bytesSchema }

// Chunks is the raw-format framing schema consumed by the identity Parser.
func Chunks() schema.Type[packet.Chunk] { return chunkSchema }

// Packets is the linear-PCM codec packet schema consumed by Decoder.
func Packets() schema.Type[packet.Packet] { return packetSchema }
