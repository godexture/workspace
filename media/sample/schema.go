package sample

import (
	"github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/schema"
)

type (
	s16SchemaID struct{}
	s32SchemaID struct{}
	f32SchemaID struct{}
	f64SchemaID struct{}
)

// The four canonical decoded frame schemas, one per Go scalar type. A wire
// coding narrower than its canonical type keeps its effective depth in
// Description.ValidBits rather than in a schema of its own.
var frameSchemas = map[Coding]any{
	S16: defineFrames[s16SchemaID, int16](),
	S32: defineFrames[s32SchemaID, int32](),
	F32: defineFrames[f32SchemaID, float32](),
	F64: defineFrames[f64SchemaID, float64](),
}

var frameDescriptors = erasedFrameSchemas()

func defineFrames[Marker any, S audio.Sample]() schema.Type[audio.Frame[S]] {
	return schema.Define[Marker](schema.Traits[audio.Frame[S]]{
		Fork: func(value audio.Frame[S]) audio.Frame[S] { return value.Share() },
		Drop: func(value audio.Frame[S]) { value.Release() },
		Size: func(value audio.Frame[S]) int { return value.Planes().Layout().Size },
		Time: func(value audio.Frame[S]) (int64, bool) {
			pts, ok := value.PTS().Get()
			return pts.Int64(), ok
		},
	})
}

func erasedFrameSchemas() map[Coding]schema.Descriptor {
	type described interface{ Descriptor() schema.Descriptor }
	result := make(map[Coding]schema.Descriptor, len(frameSchemas))
	for coding, value := range frameSchemas {
		result[coding] = value.(described).Descriptor()
	}
	return result
}

// CodingOf returns the canonical coding a frame of the scalar type S stores.
// It is empty for a type outside the four canonical representations.
func CodingOf[S audio.Sample]() Coding {
	switch any(*new(S)).(type) {
	case int16:
		return S16
	case int32:
		return S32
	case float32:
		return F32
	case float64:
		return F64
	default:
		return ""
	}
}

// Frames returns the canonical planar frame schema for the scalar type S. A
// type outside the four canonical representations yields an invalid schema,
// which the port and component that declared it report.
func Frames[S audio.Sample]() schema.Type[audio.Frame[S]] {
	typed, _ := frameSchemas[CodingOf[S]()].(schema.Type[audio.Frame[S]])
	return typed
}

// Schema returns the erased canonical frame schema that stores a decoded
// coding. Wire codings have no schema of their own; Coding.Decoded names the
// canonical coding they widen into.
func Schema(coding Coding) (schema.Descriptor, bool) {
	value, ok := frameDescriptors[coding]
	return value, ok
}

// Stores reports whether a wire coding decodes into frames of the scalar type
// S, which is what a codec component with a static frame port can accept.
func Stores[S audio.Sample](coding Coding) bool {
	return coding.Valid() && coding.Decoded() == CodingOf[S]()
}
