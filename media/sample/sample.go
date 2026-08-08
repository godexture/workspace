// Package sample defines stream-level audio sample vocabulary and canonical
// planar frame schemas. Item-local sample storage remains in media/audio.
package sample

import (
	"errors"
	"fmt"

	"github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/plugin"
)

// Format identifies both the scalar representation and its packing.
type Format string

const (
	S16Interleaved Format = "s16"
	S16Planar      Format = "s16p"
	F32Planar      Format = "f32p"
)

func (f Format) Valid() bool {
	switch f {
	case S16Interleaved, S16Planar, F32Planar:
		return true
	default:
		return false
	}
}

// Endian describes wire byte order. Typed planar frames use NoEndian because
// their scalar Go type, rather than a byte interpretation, defines values.
type Endian string

const (
	NoEndian     Endian = "none"
	LittleEndian Endian = "little"
	BigEndian    Endian = "big"
)

func (e Endian) Valid() bool {
	switch e {
	case NoEndian, LittleEndian, BigEndian:
		return true
	default:
		return false
	}
}

// Layout is an extensible channel-layout name. The initial PCM consumer
// supports the two layouts below; future codecs may add vocabulary without
// changing Frame.
type Layout string

const (
	Mono   Layout = "mono"
	Stereo Layout = "stereo"
)

func (l Layout) Channels() int {
	switch l {
	case Mono:
		return 1
	case Stereo:
		return 2
	default:
		return 0
	}
}

func (l Layout) Valid() bool { return l.Channels() != 0 }

type (
	formatKeyID struct{}
	bitsKeyID   struct{}
	rateKeyID   struct{}
	layoutKeyID struct{}
	endianKeyID struct{}
	s16SchemaID struct{}
	f32SchemaID struct{}
)

var (
	SampleFormat  = property.Define[formatKeyID](property.Scalar[Format]())
	ValidBits     = property.Define[bitsKeyID](property.Scalar[int]())
	SampleRate    = property.Define[rateKeyID](property.Scalar[int]())
	ChannelLayout = property.Define[layoutKeyID](property.Scalar[Layout]())
	ByteOrder     = property.Define[endianKeyID](property.Scalar[Endian]())

	s16 = schema.Define[s16SchemaID](schema.Traits[audio.Frame[int16]]{
		Fork: func(value audio.Frame[int16]) audio.Frame[int16] { return value.Share() },
		Drop: func(value audio.Frame[int16]) { value.Release() },
		Size: func(value audio.Frame[int16]) int { return value.Planes().Layout().Size },
		Time: frameTime[int16],
	})
	f32 = schema.Define[f32SchemaID](schema.Traits[audio.Frame[float32]]{
		Fork: func(value audio.Frame[float32]) audio.Frame[float32] { return value.Share() },
		Drop: func(value audio.Frame[float32]) { value.Release() },
		Size: func(value audio.Frame[float32]) int { return value.Planes().Layout().Size },
		Time: frameTime[float32],
	})
)

func frameTime[S audio.Sample](value audio.Frame[S]) (int64, bool) {
	pts, ok := value.PTS().Get()
	return pts.Int64(), ok
}

// S16 returns the canonical signed 16-bit planar frame schema.
func S16() schema.Type[audio.Frame[int16]] { return s16 }

// F32 returns the canonical float32 planar frame schema.
func F32() schema.Type[audio.Frame[float32]] { return f32 }

// Description is the complete stream-invariant sample representation.
type Description struct {
	Format    Format
	ValidBits int
	Rate      int
	Layout    Layout
	Endian    Endian
}

var ErrInvalidDescription = errors.New("sample description is invalid")

func (d Description) Valid() bool {
	if !d.Format.Valid() || d.ValidBits <= 0 || d.Rate <= 0 || !d.Layout.Valid() || !d.Endian.Valid() {
		return false
	}
	switch d.Format {
	case S16Interleaved:
		return d.ValidBits <= 16 && d.Endian != NoEndian
	case S16Planar:
		return d.ValidBits <= 16 && d.Endian == NoEndian
	case F32Planar:
		return d.ValidBits == 32 && d.Endian == NoEndian
	default:
		return false
	}
}

// Properties encodes the description into an immutable descriptor property
// set. Every field participates in the canonical descriptor fingerprint.
func (d Description) Properties() (property.Set, error) {
	return d.Apply(property.New())
}

// Apply replaces the five sample properties while preserving every unknown
// third-party property already present in the set.
func (d Description) Apply(result property.Set) (property.Set, error) {
	if !d.Valid() {
		return property.Set{}, ErrInvalidDescription
	}
	var err error
	result, err = property.Put(result, SampleFormat, d.Format)
	if err != nil {
		return property.Set{}, err
	}
	result, err = property.Put(result, ValidBits, d.ValidBits)
	if err != nil {
		return property.Set{}, err
	}
	result, err = property.Put(result, SampleRate, d.Rate)
	if err != nil {
		return property.Set{}, err
	}
	result, err = property.Put(result, ChannelLayout, d.Layout)
	if err != nil {
		return property.Set{}, err
	}
	result, err = property.Put(result, ByteOrder, d.Endian)
	if err != nil {
		return property.Set{}, err
	}
	return result, nil
}

// FromProperties decodes and validates a complete sample description.
func FromProperties(properties property.Set) (Description, error) {
	format, formatOK := SampleFormat.Get(properties)
	bits, bitsOK := ValidBits.Get(properties)
	rate, rateOK := SampleRate.Get(properties)
	layout, layoutOK := ChannelLayout.Get(properties)
	endian, endianOK := ByteOrder.Get(properties)
	result := Description{Format: format, ValidBits: bits, Rate: rate, Layout: layout, Endian: endian}
	if !formatOK || !bitsOK || !rateOK || !layoutOK || !endianOK || !result.Valid() {
		return Description{}, fmt.Errorf("%w: sample properties are incomplete or inconsistent", ErrInvalidDescription)
	}
	return result, nil
}

// Declarations exposes the property vocabulary to optional Host conflict
// validation. The returned slice has independent storage.
func Declarations() []plugin.Declaration {
	return []plugin.Declaration{
		plugin.DeclareKey(SampleFormat),
		plugin.DeclareKey(ValidBits),
		plugin.DeclareKey(SampleRate),
		plugin.DeclareKey(ChannelLayout),
		plugin.DeclareKey(ByteOrder),
	}
}
