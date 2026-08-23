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
var (
	s16 = defineFrames[s16SchemaID, int16]()
	s32 = defineFrames[s32SchemaID, int32]()
	f32 = defineFrames[f32SchemaID, float32]()
	f64 = defineFrames[f64SchemaID, float64]()
)

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

// Frames returns the canonical planar frame schema for the scalar type S. A
// type outside the four canonical representations yields an invalid schema,
// which the port and component that declared it report.
func Frames[S audio.Sample]() schema.Type[audio.Frame[S]] {
	var erased any
	switch any(*new(S)).(type) {
	case int16:
		erased = s16
	case int32:
		erased = s32
	case float32:
		erased = f32
	case float64:
		erased = f64
	default:
		return schema.Type[audio.Frame[S]]{}
	}
	typed, _ := erased.(schema.Type[audio.Frame[S]])
	return typed
}

// Schema returns the erased canonical frame schema that stores a decoded
// coding. Wire codings have no schema of their own; Coding.Decoded names the
// canonical coding they widen into.
func Schema(coding Coding) (schema.Descriptor, bool) {
	switch coding {
	case S16:
		return s16.Descriptor(), true
	case S32:
		return s32.Descriptor(), true
	case F32:
		return f32.Descriptor(), true
	case F64:
		return f64.Descriptor(), true
	default:
		return schema.Descriptor{}, false
	}
}
