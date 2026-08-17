package codec

import (
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/schema"
)

type packetSchemaID struct{}

var packetSchema = schema.Define[packetSchemaID](schema.Traits[packet.Packet]{
	Fork: func(value packet.Packet) packet.Packet { return value.Share() },
	Drop: func(value packet.Packet) { value.Release() },
	Size: func(value packet.Packet) int { return value.Bytes().Len() },
	Time: func(value packet.Packet) (int64, bool) {
		pts, ok := value.PTS().Get()
		return pts.Int64(), ok
	},
	Order: func(value packet.Packet) (int64, bool) {
		dts, ok := value.DTS().Get()
		return dts.Int64(), ok
	},
})

// Packets is the canonical codec-packet schema shared by parsers and codecs.
func Packets() schema.Type[packet.Packet] { return packetSchema }
