// Package timing defines stream-local integer time bases and typed timestamps.
package timing

import (
	"errors"
	"fmt"
	"math"
	"math/bits"
)

var (
	ErrInvalidBase     = errors.New("time base must have positive numerator and denominator")
	ErrInvalidRounding = errors.New("invalid rounding mode")
	ErrOverflow        = errors.New("timestamp rescale overflow")
)

// Base describes the duration of one stream tick as Numerator/Denominator
// seconds. Both fields are positive and are intentionally bounded integers.
type Base struct {
	Numerator   int64
	Denominator int64
}

func NewBase(numerator, denominator int64) (Base, error) {
	base := Base{Numerator: numerator, Denominator: denominator}
	if !base.Valid() {
		return Base{}, ErrInvalidBase
	}
	return base, nil
}

func MustBase(numerator, denominator int64) Base {
	base, err := NewBase(numerator, denominator)
	if err != nil {
		panic(err)
	}
	return base
}

func (b Base) Valid() bool { return b.Numerator > 0 && b.Denominator > 0 }

func (b Base) String() string { return fmt.Sprintf("%d/%d", b.Numerator, b.Denominator) }

// Rounding is the explicit policy used when a rational result is not an
// integer. There is no implicit truncation mode.
type Rounding uint8

const (
	RoundTowardZero Rounding = iota
	RoundAwayFromZero
	RoundFloor
	RoundCeil
	RoundNearestEven
	RoundNearestAway
)

const (
	RoundZero    = RoundTowardZero
	RoundNearest = RoundNearestEven
)

// Rescale converts a value in b's time base into target's time base with
// checked integer arithmetic.
func (b Base) Rescale(value int64, target Base, rounding Rounding) (int64, error) {
	return rescale(value, b, target, rounding)
}

// PTS, DTS, and Duration are intentionally distinct types.
type PTS int64
type DTS int64
type Duration int64

func NewPTS(value int64) PTS           { return PTS(value) }
func NewDTS(value int64) DTS           { return DTS(value) }
func NewDuration(value int64) Duration { return Duration(value) }
func (v PTS) Int64() int64             { return int64(v) }
func (v DTS) Int64() int64             { return int64(v) }
func (v Duration) Int64() int64        { return int64(v) }
func (v PTS) Rescale(from, to Base, mode Rounding) (PTS, error) {
	value, err := rescale(int64(v), from, to, mode)
	return PTS(value), err
}
func (v DTS) Rescale(from, to Base, mode Rounding) (DTS, error) {
	value, err := rescale(int64(v), from, to, mode)
	return DTS(value), err
}
func (v Duration) Rescale(from, to Base, mode Rounding) (Duration, error) {
	value, err := rescale(int64(v), from, to, mode)
	return Duration(value), err
}

func RescalePTS(value PTS, from, to Base, mode Rounding) (PTS, error) {
	return value.Rescale(from, to, mode)
}

func RescaleDTS(value DTS, from, to Base, mode Rounding) (DTS, error) {
	return value.Rescale(from, to, mode)
}

func RescaleDuration(value Duration, from, to Base, mode Rounding) (Duration, error) {
	return value.Rescale(from, to, mode)
}

type OptionalPTS struct {
	value PTS
	valid bool
}

type OptionalDTS struct {
	value DTS
	valid bool
}

type OptionalDuration struct {
	value Duration
	valid bool
}

func SomePTS(value PTS) OptionalPTS { return OptionalPTS{value: value, valid: true} }
func UnknownPTS() OptionalPTS       { return OptionalPTS{} }
func SomeDTS(value DTS) OptionalDTS { return OptionalDTS{value: value, valid: true} }
func UnknownDTS() OptionalDTS       { return OptionalDTS{} }
func SomeDuration(value Duration) OptionalDuration {
	return OptionalDuration{value: value, valid: true}
}
func UnknownDuration() OptionalDuration { return OptionalDuration{} }

func (v OptionalPTS) Valid() bool      { return v.valid }
func (v OptionalDTS) Valid() bool      { return v.valid }
func (v OptionalDuration) Valid() bool { return v.valid }

func (v OptionalPTS) Get() (PTS, bool)           { return v.value, v.valid }
func (v OptionalDTS) Get() (DTS, bool)           { return v.value, v.valid }
func (v OptionalDuration) Get() (Duration, bool) { return v.value, v.valid }

func (v OptionalPTS) Value() PTS           { return v.value }
func (v OptionalDTS) Value() DTS           { return v.value }
func (v OptionalDuration) Value() Duration { return v.value }
func (v OptionalPTS) Rescale(from, to Base, mode Rounding) (OptionalPTS, error) {
	if !v.valid {
		return v, nil
	}
	value, err := v.value.Rescale(from, to, mode)
	return SomePTS(value), err
}
func (v OptionalDTS) Rescale(from, to Base, mode Rounding) (OptionalDTS, error) {
	if !v.valid {
		return v, nil
	}
	value, err := v.value.Rescale(from, to, mode)
	return SomeDTS(value), err
}
func (v OptionalDuration) Rescale(from, to Base, mode Rounding) (OptionalDuration, error) {
	if !v.valid {
		return v, nil
	}
	value, err := v.value.Rescale(from, to, mode)
	return SomeDuration(value), err
}

type uint128 struct {
	hi uint64
	lo uint64
}

func (v uint128) isZero() bool { return v.hi == 0 && v.lo == 0 }

func compare128(left, right uint128) int {
	if left.hi < right.hi {
		return -1
	}
	if left.hi > right.hi {
		return 1
	}
	if left.lo < right.lo {
		return -1
	}
	if left.lo > right.lo {
		return 1
	}
	return 0
}

func subtract128(left, right uint128) uint128 {
	lo, borrow := bits.Sub64(left.lo, right.lo, 0)
	hi, _ := bits.Sub64(left.hi, right.hi, borrow)
	return uint128{hi: hi, lo: lo}
}

func multiplyFactors(factors []uint64) (uint128, bool) {
	result := uint128{lo: 1}
	for _, factor := range factors {
		hi, lo := bits.Mul64(result.lo, factor)
		high, _ := bits.Mul64(result.hi, factor)
		sum, carry := bits.Add64(hi, high, 0)
		if carry != 0 {
			return uint128{}, false
		}
		result = uint128{hi: sum, lo: lo}
	}
	return result, true
}

func divide128(numerator, denominator uint128) (uint128, uint128) {
	var quotient uint128
	var remainder uint128
	for bit := 127; bit >= 0; bit-- {
		carry := remainder.hi >> 63
		remainder = uint128{
			hi: remainder.hi<<1 | remainder.lo>>63,
			lo: remainder.lo << 1,
		}
		if bitAt(numerator, bit) {
			remainder.lo |= 1
		}
		if carry != 0 || compare128(remainder, denominator) >= 0 {
			remainder = subtract128(remainder, denominator)
			setBit(&quotient, bit)
		}
	}
	return quotient, remainder
}

func bitAt(value uint128, bit int) bool {
	if bit >= 64 {
		return value.hi&(uint64(1)<<(bit-64)) != 0
	}
	return value.lo&(uint64(1)<<bit) != 0
}

func setBit(value *uint128, bit int) {
	if bit >= 64 {
		value.hi |= uint64(1) << (bit - 64)
		return
	}
	value.lo |= uint64(1) << bit
}

func half(value uint128) uint128 {
	return uint128{hi: value.hi >> 1, lo: value.lo>>1 | value.hi<<63}
}

func gcd(left, right uint64) uint64 {
	for right != 0 {
		left, right = right, left%right
	}
	return left
}

func reduceFactors(numerator, denominator []uint64) {
	for numeratorIndex := range numerator {
		for denominatorIndex := range denominator {
			factor := gcd(numerator[numeratorIndex], denominator[denominatorIndex])
			numerator[numeratorIndex] /= factor
			denominator[denominatorIndex] /= factor
		}
	}
}

func rescale(value int64, from, to Base, mode Rounding) (int64, error) {
	if !from.Valid() || !to.Valid() {
		return 0, ErrInvalidBase
	}
	if mode > RoundNearestAway {
		return 0, ErrInvalidRounding
	}
	if value == 0 {
		return 0, nil
	}

	negative := value < 0
	magnitude := uint64(value)
	if negative {
		magnitude = uint64(-(value + 1)) + 1
	}
	numerator := []uint64{magnitude, uint64(from.Numerator), uint64(to.Denominator)}
	denominator := []uint64{uint64(from.Denominator), uint64(to.Numerator)}
	reduceFactors(numerator, denominator)
	numeratorValue, ok := multiplyFactors(numerator)
	if !ok {
		return 0, ErrOverflow
	}
	denominatorValue, ok := multiplyFactors(denominator)
	if !ok || denominatorValue.isZero() {
		return 0, ErrOverflow
	}
	quotient, remainder := divide128(numeratorValue, denominatorValue)
	if quotient.hi != 0 {
		return 0, ErrOverflow
	}
	increment := shouldIncrement(quotient, remainder, denominatorValue, negative, mode)
	if increment {
		if quotient.lo == math.MaxUint64 {
			return 0, ErrOverflow
		}
		quotient.lo++
	}
	if !negative {
		if quotient.lo > math.MaxInt64 {
			return 0, ErrOverflow
		}
		return int64(quotient.lo), nil
	}
	if quotient.lo > uint64(math.MaxInt64)+1 {
		return 0, ErrOverflow
	}
	if quotient.lo == uint64(math.MaxInt64)+1 {
		return math.MinInt64, nil
	}
	return -int64(quotient.lo), nil
}

func shouldIncrement(quotient, remainder, denominator uint128, negative bool, mode Rounding) bool {
	if remainder.isZero() {
		return false
	}
	switch mode {
	case RoundTowardZero:
		return false
	case RoundAwayFromZero:
		return true
	case RoundFloor:
		return negative
	case RoundCeil:
		return !negative
	case RoundNearestEven, RoundNearestAway:
		halfDenominator := half(denominator)
		comparison := compare128(remainder, halfDenominator)
		if denominator.lo&1 == 0 {
			if comparison > 0 {
				return true
			}
			if comparison < 0 {
				return false
			}
			return mode == RoundNearestAway || quotient.lo&1 == 1
		}
		return comparison >= 0
	default:
		return false
	}
}
