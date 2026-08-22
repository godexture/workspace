package sample

import (
	"math/bits"
	"strconv"
	"strings"
)

// Position is a speaker position. Values are the bit indices of the channel
// mask WAVE, MP4 and Matroska all inherited from WAVEFORMATEXTENSIBLE.
type Position uint8

const (
	FrontLeft Position = iota
	FrontRight
	FrontCenter
	LowFrequency
	BackLeft
	BackRight
	FrontLeftOfCenter
	FrontRightOfCenter
	BackCenter
	SideLeft
	SideRight
	TopCenter
	TopFrontLeft
	TopFrontCenter
	TopFrontRight
	TopBackLeft
	TopBackCenter
	TopBackRight
	positionCount
)

var positionNames = [positionCount]string{
	"FL", "FR", "FC", "LFE", "BL", "BR", "FLC", "FRC", "BC",
	"SL", "SR", "TC", "TFL", "TFC", "TFR", "TBL", "TBC", "TBR",
}

func (p Position) Valid() bool { return p < positionCount }

func (p Position) String() string {
	if !p.Valid() {
		return "position(" + strconv.Itoa(int(p)) + ")"
	}
	return positionNames[p]
}

func (p Position) bit() uint32 { return 1 << uint32(p) }

// MaxChannels bounds a layout so channel counts stay inside plane bookkeeping
// and one byte of storage.
const MaxChannels = 64

// Layout is a comparable channel layout. It names either a set of speaker
// positions or a bare channel count, so a source that does not describe its
// positions is representable without inventing them.
type Layout struct {
	mask  uint32
	count uint8
}

// Positions builds a layout from distinct speaker positions. A repeated or
// unknown position yields the zero Layout, which Description rejects.
func Positions(values ...Position) Layout {
	if len(values) == 0 || len(values) > int(positionCount) {
		return Layout{}
	}
	mask := uint32(0)
	for _, value := range values {
		if !value.Valid() || mask&value.bit() != 0 {
			return Layout{}
		}
		mask |= value.bit()
	}
	return Layout{mask: mask, count: uint8(len(values))}
}

// Channels builds a layout of count channels whose positions are unknown.
func Channels(count int) Layout {
	if count <= 0 || count > MaxChannels {
		return Layout{}
	}
	return Layout{count: uint8(count)}
}

// FromMask builds a layout from a channel mask and the channel count that
// accompanies it. A zero mask keeps the count and reports no positions; a mask
// that disagrees with count is rejected rather than silently discarded.
func FromMask(mask uint32, count int) (Layout, bool) {
	result := Channels(count)
	if !result.Valid() {
		return Layout{}, false
	}
	if mask == 0 {
		return result, true
	}
	if mask>>positionCount != 0 || bits.OnesCount32(mask) != count {
		return Layout{}, false
	}
	result.mask = mask
	return result, true
}

func Mono() Layout   { return Positions(FrontCenter) }
func Stereo() Layout { return Positions(FrontLeft, FrontRight) }

func (l Layout) Valid() bool { return l.count != 0 }
func (l Layout) Count() int  { return int(l.count) }

// Positioned reports whether this layout names its speaker positions.
func (l Layout) Positioned() bool { return l.mask != 0 }

// Mask returns the channel mask, or zero when the positions are unknown.
func (l Layout) Mask() uint32 { return l.mask }

func (l Layout) Has(value Position) bool {
	return value.Valid() && l.mask&value.bit() != 0
}

// At returns the position of one channel in mask order, which is the order
// interleaved samples are stored in.
func (l Layout) At(index int) (Position, bool) {
	if index < 0 || index >= l.Count() || !l.Positioned() {
		return 0, false
	}
	remaining := l.mask
	for range index {
		remaining &= remaining - 1
	}
	return Position(bits.TrailingZeros32(remaining)), true
}

func (l Layout) String() string {
	if !l.Valid() {
		return ""
	}
	if !l.Positioned() {
		return strconv.Itoa(l.Count()) + "ch"
	}
	var builder strings.Builder
	for index := range l.Count() {
		if index != 0 {
			builder.WriteByte('+')
		}
		position, _ := l.At(index)
		builder.WriteString(position.String())
	}
	return builder.String()
}

// layoutAliases are the friendly names an operator types. String never emits
// them, so the canonical text stays one form per layout.
var layoutAliases = map[string]func() Layout{"mono": Mono, "stereo": Stereo}

// ParseLayout reads back the text String produces: a channel count such as
// "6ch", or positions joined by "+" such as "FL+FR+FC". The names "mono" and
// "stereo" are accepted as aliases. Matching ignores case so a hand-written
// config is forgiving.
func ParseLayout(text string) (Layout, bool) {
	if text == "" {
		return Layout{}, false
	}
	if alias, ok := layoutAliases[strings.ToLower(text)]; ok {
		return alias(), true
	}
	if count, found := strings.CutSuffix(text, "ch"); found {
		value, err := strconv.Atoi(count)
		if err != nil {
			return Layout{}, false
		}
		layout := Channels(value)
		return layout, layout.Valid()
	}
	names := strings.Split(text, "+")
	values := make([]Position, 0, len(names))
	for _, name := range names {
		position, ok := parsePosition(name)
		if !ok {
			return Layout{}, false
		}
		values = append(values, position)
	}
	layout := Positions(values...)
	return layout, layout.Valid()
}

func parsePosition(name string) (Position, bool) {
	for index, candidate := range positionNames {
		if strings.EqualFold(name, candidate) {
			return Position(index), true
		}
	}
	return 0, false
}
