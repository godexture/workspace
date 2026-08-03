package config

import (
	"fmt"
	"strconv"

	"github.com/godexture/godec/diagnostic"
)

// ByteValue is an integer quantity measured in bytes.
type ByteValue int64

// FrequencyValue is a frequency measured in hertz.
type FrequencyValue int64

// RatioValue is a dimensionless ratio.
type RatioValue float64

// DecibelValue is an amplitude or power value in decibels, as defined by the
// component using it.
type DecibelValue float64

// Bytes returns a byte-size codec.
func Bytes() Codec[ByteValue] {
	return unitInt64Codec("bytes", "bytes", func(value ByteValue) string { return strconv.FormatInt(int64(value), 10) })
}

// Frequency returns a frequency codec.
func Frequency() Codec[FrequencyValue] {
	return unitInt64Codec("frequency", "Hz", func(value FrequencyValue) string { return strconv.FormatInt(int64(value), 10) })
}

// Ratio returns a ratio codec.
func Ratio() Codec[RatioValue] {
	return unitFloat64Codec[RatioValue]("ratio", "ratio")
}

// Decibel returns a decibel codec.
func Decibel() Codec[DecibelValue] {
	return unitFloat64Codec[DecibelValue]("decibel", "dB")
}

func unitInt64Codec[T ~int64](typ, unit string, encode func(T) string) Codec[T] {
	return NewCodec(CodecSpec[T]{
		Type:   typ,
		Decode: parseInt[T],
		Encode: encode,
		Canonical: func(value T) ([]byte, error) {
			return []byte(typ + ":" + strconv.FormatInt(int64(value), 10)), nil
		},
		Description: Description{Type: typ, Unit: unit},
	})
}

func unitFloat64Codec[T ~float64](typ, unit string) Codec[T] {
	return NewCodec(CodecSpec[T]{
		Type: "float<" + typ + ">",
		Decode: func(value string) (T, error) {
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil || !finite(parsed) {
				return 0, fmt.Errorf("value must be a finite %s", typ)
			}
			return T(parsed), nil
		},
		Encode: func(value T) string { return strconv.FormatFloat(float64(value), 'g', -1, 64) },
		Canonical: func(value T) ([]byte, error) {
			if !finite(float64(value)) {
				return nil, fmt.Errorf("value must be finite")
			}
			if value == 0 {
				value = 0
			}
			return []byte(typ + ":" + strconv.FormatFloat(float64(value), 'g', -1, 64)), nil
		},
		Validate: func(value T) []diagnostic.Item {
			if !finite(float64(value)) {
				return []diagnostic.Item{diagnostic.NewItem("config.non-finite", diagnostic.ErrorSeverity, diagnostic.Path{}, "value must be finite", nil)}
			}
			return nil
		},
		Description: Description{Type: typ, Unit: unit},
	})
}
