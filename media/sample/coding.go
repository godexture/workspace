// Package sample defines the stream-level audio sample vocabulary. Scalar
// representation, packing and byte order are independent axes; item-local
// sample storage and the canonical frame schemas live in media/audio.
package sample

// Coding is the scalar representation of one sample.
type Coding string

const (
	U8  Coding = "u8"
	S8  Coding = "s8"
	S16 Coding = "s16"
	S24 Coding = "s24"
	S32 Coding = "s32"
	F32 Coding = "f32"
	F64 Coding = "f64"
)

// Bytes reports the width of one stored sample. Zero means the coding is not
// part of the vocabulary.
func (c Coding) Bytes() int {
	switch c {
	case U8, S8:
		return 1
	case S16:
		return 2
	case S24:
		return 3
	case S32, F32:
		return 4
	case F64:
		return 8
	default:
		return 0
	}
}

func (c Coding) Bits() int   { return c.Bytes() * 8 }
func (c Coding) Valid() bool { return c.Bytes() != 0 }
func (c Coding) Float() bool { return c == F32 || c == F64 }

// Decoded reports the canonical planar coding this wire coding decodes into.
// Narrower codings widen into the next Go scalar type and keep their effective
// depth in Description.ValidBits rather than in a schema of their own.
func (c Coding) Decoded() Coding {
	switch c {
	case U8, S8, S16:
		return S16
	case S24, S32:
		return S32
	case F32:
		return F32
	case F64:
		return F64
	default:
		return ""
	}
}

// Canonical reports whether a decoded frame can be stored in this coding.
func (c Coding) Canonical() bool { return c.Valid() && c.Decoded() == c }

// Packing is how consecutive samples of different channels are laid out.
type Packing string

const (
	Interleaved Packing = "interleaved"
	Planar      Packing = "planar"
)

func (p Packing) Valid() bool { return p == Interleaved || p == Planar }

// Endian is the byte order of a multi-byte interleaved wire sample. Planar
// frames and single-byte codings use NoEndian because a Go scalar type, not a
// byte interpretation, defines their values.
type Endian string

const (
	NoEndian     Endian = "none"
	LittleEndian Endian = "little"
	BigEndian    Endian = "big"
)

func (e Endian) Valid() bool { return e == NoEndian || e == LittleEndian || e == BigEndian }
