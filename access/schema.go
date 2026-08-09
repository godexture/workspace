package access

import (
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/schema"
)

type bytesSchemaID struct{}

var bytesSchema = schema.Define[bytesSchemaID](schema.Traits[buffer.Handle]{
	Fork: func(value buffer.Handle) buffer.Handle { return value.Share() },
	Drop: func(value buffer.Handle) { value.Release() },
	Size: func(value buffer.Handle) int { return value.Layout().Size },
})

// Bytes is the canonical byte-stream schema shared by Access and Format components.
func Bytes() schema.Type[buffer.Handle] { return bytesSchema }
