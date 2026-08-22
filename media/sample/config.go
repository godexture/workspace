package sample

import (
	"fmt"

	"github.com/godexture/godec/config"
)

// LayoutCodec is the control-plane representation of a channel layout. Every
// component that lets an operator name channels shares it, so "FL+FR" and
// "6ch" mean the same thing on every surface.
func LayoutCodec() config.Codec[Layout] {
	return config.NewCodec(config.CodecSpec[Layout]{
		Type: "sample.layout",
		Decode: func(text string) (Layout, error) {
			value, ok := ParseLayout(text)
			if !ok {
				return Layout{}, fmt.Errorf("channel layout %q is not a channel count or a set of positions", text)
			}
			return value, nil
		},
		Encode: Layout.String,
		Canonical: func(value Layout) ([]byte, error) {
			if !value.Valid() {
				return nil, fmt.Errorf("channel layout is invalid")
			}
			return []byte("layout:" + value.String()), nil
		},
		Description: config.Description{Help: "channel count such as 6ch, or positions such as FL+FR+FC+LFE+BL+BR"},
	})
}

// CodingCodec enumerates the scalar sample representations.
func CodingCodec() config.Codec[Coding] {
	return config.Enum(
		config.Choice[Coding]{ID: string(U8), Label: "Unsigned 8-bit", Value: U8},
		config.Choice[Coding]{ID: string(S8), Label: "Signed 8-bit", Value: S8},
		config.Choice[Coding]{ID: string(S16), Label: "Signed 16-bit", Value: S16},
		config.Choice[Coding]{ID: string(S24), Label: "Signed 24-bit", Value: S24},
		config.Choice[Coding]{ID: string(S32), Label: "Signed 32-bit", Value: S32},
		config.Choice[Coding]{ID: string(F32), Label: "32-bit float", Value: F32},
		config.Choice[Coding]{ID: string(F64), Label: "64-bit float", Value: F64},
	)
}

// EndianCodec enumerates the byte orders an interleaved wire coding can use.
// Planar frames and single-byte codings carry NoEndian, which no operator
// selects, so it is not offered here.
func EndianCodec() config.Codec[Endian] {
	return config.Enum(
		config.Choice[Endian]{ID: string(LittleEndian), Label: "Little endian", Value: LittleEndian},
		config.Choice[Endian]{ID: string(BigEndian), Label: "Big endian", Value: BigEndian},
	)
}
