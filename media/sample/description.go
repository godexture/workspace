package sample

import (
	"errors"
	"fmt"

	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/plugin"
)

type (
	codingKeyID  struct{}
	packingKeyID struct{}
	endianKeyID  struct{}
	rateKeyID    struct{}
	layoutKeyID  struct{}
	bitsKeyID    struct{}
)

var (
	sampleCoding  = property.Define[codingKeyID](property.Scalar[Coding]())
	samplePacking = property.Define[packingKeyID](property.Scalar[Packing]())
	byteOrder     = property.Define[endianKeyID](property.Scalar[Endian]())
	sampleRate    = property.Define[rateKeyID](property.Scalar[int]())
	channelLayout = property.Define[layoutKeyID](func(value Layout) ([]byte, error) {
		if !value.Valid() {
			return nil, errors.New("channel layout is invalid")
		}
		return []byte("layout:" + value.String()), nil
	})
	validBits = property.Define[bitsKeyID](property.Scalar[int]())
)

func SampleCoding() property.Key[Coding]   { return sampleCoding }
func SamplePacking() property.Key[Packing] { return samplePacking }
func ByteOrder() property.Key[Endian]      { return byteOrder }
func SampleRate() property.Key[int]        { return sampleRate }
func ChannelLayout() property.Key[Layout]  { return channelLayout }
func ValidBits() property.Key[int]         { return validBits }

// Description is the complete stream-invariant sample representation.
//
// Decoded frames are stored at the full scale of their canonical coding, so a
// wire coding narrower than its container is most-significant-bit aligned as
// WAVEFORMATEXTENSIBLE defines it. ValidBits records how many of those bits
// carry information; it never changes how a value is scaled.
type Description struct {
	Coding    Coding
	Packing   Packing
	Endian    Endian
	Rate      int
	Layout    Layout
	ValidBits int
}

var ErrInvalidDescription = errors.New("sample description is invalid")

func (d Description) Valid() bool {
	if !d.Coding.Valid() || !d.Packing.Valid() || !d.Layout.Valid() || d.Rate <= 0 {
		return false
	}
	if d.ValidBits <= 0 || d.ValidBits > d.Coding.Bits() {
		return false
	}
	if d.Coding.Float() && d.ValidBits != d.Coding.Bits() {
		return false
	}
	if d.Packing == Planar {
		return d.Coding.Canonical() && d.Endian == NoEndian
	}
	if d.Coding.Bytes() == 1 {
		return d.Endian == NoEndian
	}
	return d.Endian == LittleEndian || d.Endian == BigEndian
}

// Decoded returns the canonical planar description a decoder produces for this
// wire description.
func (d Description) Decoded() Description {
	d.Coding, d.Packing, d.Endian = d.Coding.Decoded(), Planar, NoEndian
	return d
}

// BlockBytes is the size of one interleaved sample frame across all channels.
func (d Description) BlockBytes() int { return d.Coding.Bytes() * d.Layout.Count() }

// Properties encodes the description into an immutable descriptor property
// set. Every field participates in the canonical descriptor fingerprint.
func (d Description) Properties() (property.Set, error) {
	return d.Apply(property.New())
}

// Apply replaces the sample properties while preserving every unknown
// third-party property already present in the set.
func (d Description) Apply(result property.Set) (property.Set, error) {
	if !d.Valid() {
		return property.Set{}, ErrInvalidDescription
	}
	accumulator := putter{set: result}
	put(&accumulator, sampleCoding, d.Coding)
	put(&accumulator, samplePacking, d.Packing)
	put(&accumulator, byteOrder, d.Endian)
	put(&accumulator, sampleRate, d.Rate)
	put(&accumulator, channelLayout, d.Layout)
	put(&accumulator, validBits, d.ValidBits)
	if accumulator.err != nil {
		return property.Set{}, accumulator.err
	}
	return accumulator.set, nil
}

// putter accumulates typed puts so Apply stays a flat list of keys instead
// of six repetitions of the same error check.
type putter struct {
	set property.Set
	err error
}

func put[T any](target *putter, key property.Key[T], value T) {
	if target.err != nil {
		return
	}
	target.set, target.err = property.Put(target.set, key, value)
}

// FromProperties decodes and validates a complete sample description.
func FromProperties(properties property.Set) (Description, error) {
	coding, codingOK := sampleCoding.Get(properties)
	packing, packingOK := samplePacking.Get(properties)
	endian, endianOK := byteOrder.Get(properties)
	rate, rateOK := sampleRate.Get(properties)
	layout, layoutOK := channelLayout.Get(properties)
	bits, bitsOK := validBits.Get(properties)
	result := Description{Coding: coding, Packing: packing, Endian: endian, Rate: rate, Layout: layout, ValidBits: bits}
	if !codingOK || !packingOK || !endianOK || !rateOK || !layoutOK || !bitsOK || !result.Valid() {
		return Description{}, fmt.Errorf("%w: sample properties are incomplete or inconsistent", ErrInvalidDescription)
	}
	return result, nil
}

// Declarations exposes the property vocabulary to optional Host conflict
// validation. The returned slice has independent storage.
func Declarations() []plugin.Declaration {
	return []plugin.Declaration{
		plugin.DeclareKey(sampleCoding),
		plugin.DeclareKey(samplePacking),
		plugin.DeclareKey(byteOrder),
		plugin.DeclareKey(sampleRate),
		plugin.DeclareKey(channelLayout),
		plugin.DeclareKey(validBits),
	}
}
