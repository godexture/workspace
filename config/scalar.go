package config

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/godexture/godec/diagnostic"
)

// Bool returns a boolean codec.
func Bool() Codec[bool] {
	return NewCodec(CodecSpec[bool]{
		Type:   "bool",
		Decode: strconv.ParseBool,
		Encode: func(value bool) string { return strconv.FormatBool(value) },
		Canonical: func(value bool) ([]byte, error) {
			if value {
				return []byte("bool:1"), nil
			}
			return []byte("bool:0"), nil
		},
		Description: Description{Type: "bool"},
	})
}

// String returns a UTF-8 string codec.
func String() Codec[string] {
	return NewCodec(CodecSpec[string]{
		Type: "string",
		Decode: func(value string) (string, error) {
			if !utf8.ValidString(value) {
				return "", fmt.Errorf("string must be valid UTF-8")
			}
			return value, nil
		},
		Encode: func(value string) string { return value },
		Canonical: func(value string) ([]byte, error) {
			if !utf8.ValidString(value) {
				return nil, fmt.Errorf("string must be valid UTF-8")
			}
			return append([]byte("string:"), value...), nil
		},
		Description: Description{Type: "string"},
	})
}

// Int returns a signed integer codec.
func Int() Codec[int] {
	return NewCodec(CodecSpec[int]{
		Type:        "int",
		Decode:      parseInt[int],
		Encode:      strconv.Itoa,
		Canonical:   canonicalInt[int],
		Description: Description{Type: "int"},
	})
}

// Int64 returns a 64-bit signed integer codec.
func Int64() Codec[int64] {
	return NewCodec(CodecSpec[int64]{
		Type:        "int64",
		Decode:      parseInt[int64],
		Encode:      func(value int64) string { return strconv.FormatInt(value, 10) },
		Canonical:   canonicalInt[int64],
		Description: Description{Type: "int64"},
	})
}

// Uint returns an unsigned integer codec.
func Uint() Codec[uint] {
	return NewCodec(CodecSpec[uint]{
		Type:        "uint",
		Decode:      parseUint[uint],
		Encode:      func(value uint) string { return strconv.FormatUint(uint64(value), 10) },
		Canonical:   canonicalUint[uint],
		Description: Description{Type: "uint"},
	})
}

// Uint64 returns a 64-bit unsigned integer codec.
func Uint64() Codec[uint64] {
	return NewCodec(CodecSpec[uint64]{
		Type:        "uint64",
		Decode:      parseUint[uint64],
		Encode:      func(value uint64) string { return strconv.FormatUint(value, 10) },
		Canonical:   canonicalUint[uint64],
		Description: Description{Type: "uint64"},
	})
}

// Float32 returns a finite float32 codec. NaN and infinities are rejected.
func Float32() Codec[float32] {
	return NewCodec(CodecSpec[float32]{
		Type: "float32",
		Decode: func(value string) (float32, error) {
			parsed, err := strconv.ParseFloat(value, 32)
			if err != nil || !finite(parsed) {
				return 0, fmt.Errorf("value must be a finite float32")
			}
			return float32(parsed), nil
		},
		Encode: func(value float32) string { return strconv.FormatFloat(float64(value), 'g', -1, 32) },
		Canonical: func(value float32) ([]byte, error) {
			if !finite(float64(value)) {
				return nil, fmt.Errorf("value must be finite")
			}
			if value == 0 {
				value = 0
			}
			return []byte("float32:" + strconv.FormatFloat(float64(value), 'g', -1, 32)), nil
		},
		Normalize: func(value float32) (float32, []diagnostic.Item) {
			if value == 0 && math.Signbit(float64(value)) {
				return 0, []diagnostic.Item{diagnostic.NewItem("config.normalize-negative-zero", diagnostic.InfoSeverity, diagnostic.Path{}, "normalized negative zero", nil)}
			}
			return value, nil
		},
		Validate: func(value float32) []diagnostic.Item {
			if !finite(float64(value)) {
				return []diagnostic.Item{diagnostic.NewItem("config.non-finite", diagnostic.ErrorSeverity, diagnostic.Path{}, "value must be finite", nil)}
			}
			return nil
		},
		Description: Description{Type: "float32"},
	})
}

// Float64 returns a finite float64 codec. NaN and infinities are rejected.
func Float64() Codec[float64] {
	return NewCodec(CodecSpec[float64]{
		Type: "float64",
		Decode: func(value string) (float64, error) {
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil || !finite(parsed) {
				return 0, fmt.Errorf("value must be a finite float64")
			}
			return parsed, nil
		},
		Encode: func(value float64) string { return strconv.FormatFloat(value, 'g', -1, 64) },
		Canonical: func(value float64) ([]byte, error) {
			if !finite(value) {
				return nil, fmt.Errorf("value must be finite")
			}
			if value == 0 {
				value = 0
			}
			return []byte("float64:" + strconv.FormatFloat(value, 'g', -1, 64)), nil
		},
		Normalize: func(value float64) (float64, []diagnostic.Item) {
			if value == 0 && math.Signbit(value) {
				return 0, []diagnostic.Item{diagnostic.NewItem("config.normalize-negative-zero", diagnostic.InfoSeverity, diagnostic.Path{}, "normalized negative zero", nil)}
			}
			return value, nil
		},
		Validate: func(value float64) []diagnostic.Item {
			if !finite(value) {
				return []diagnostic.Item{diagnostic.NewItem("config.non-finite", diagnostic.ErrorSeverity, diagnostic.Path{}, "value must be finite", nil)}
			}
			return nil
		},
		Description: Description{Type: "float64"},
	})
}

// Duration returns a time.Duration codec with nanosecond canonical encoding.
func Duration() Codec[time.Duration] {
	return NewCodec(CodecSpec[time.Duration]{
		Type:   "duration",
		Decode: time.ParseDuration,
		Encode: func(value time.Duration) string { return value.String() },
		Canonical: func(value time.Duration) ([]byte, error) {
			return []byte("duration:" + strconv.FormatInt(int64(value), 10)), nil
		},
		Description: Description{Type: "duration", Unit: "duration"},
	})
}

func parseInt[T ~int | ~int64 | ~int32 | ~int16 | ~int8](value string) (T, error) {
	parsed, err := strconv.ParseInt(value, 10, reflect.TypeFor[T]().Bits())
	return T(parsed), err
}

func parseUint[T ~uint | ~uint64 | ~uint32 | ~uint16 | ~uint8](value string) (T, error) {
	parsed, err := strconv.ParseUint(value, 10, reflect.TypeFor[T]().Bits())
	return T(parsed), err
}

func canonicalInt[T ~int | ~int64 | ~int32 | ~int16 | ~int8](value T) ([]byte, error) {
	return []byte("int:" + strconv.FormatInt(int64(value), 10)), nil
}

func canonicalUint[T ~uint | ~uint64 | ~uint32 | ~uint16 | ~uint8](value T) ([]byte, error) {
	return []byte("uint:" + strconv.FormatUint(uint64(value), 10)), nil
}
