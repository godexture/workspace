package access

import (
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/schema"
)

type (
	bytesSchemaID  struct{}
	writesSchemaID struct{}
)

var bytesSchema = schema.Define[bytesSchemaID](schema.Traits[buffer.Handle]{
	Fork: func(value buffer.Handle) buffer.Handle { return value.Share() },
	Drop: func(value buffer.Handle) { value.Release() },
	Size: func(value buffer.Handle) int { return value.Layout().Size },
})

var writesSchema = schema.Define[writesSchemaID](schema.Traits[Write]{
	Fork: func(value Write) Write { return value.Share() },
	Drop: func(value Write) { value.Release() },
	Size: func(value Write) int { return value.Payload().Layout().Size },
})

// Bytes is the canonical byte-stream schema shared by Access and Format components.
func Bytes() schema.Type[buffer.Handle] { return bytesSchema }

// Writes is the canonical positioned-write schema shared by Format and Access sinks.
func Writes() schema.Type[Write] { return writesSchema }
