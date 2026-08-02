package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
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

// Choice associates a stable ID and display label with an enum value.
type Choice[T comparable] struct {
	ID    string
	Label string
	Value T
}

// Enum returns a codec whose canonical representation is the choice ID.
func Enum[T comparable](choices ...Choice[T]) Codec[T] {
	result := NewCodec(CodecSpec[T]{
		Type: "enum",
		Decode: func(value string) (T, error) {
			for _, choice := range choices {
				if choice.ID == value {
					return choice.Value, nil
				}
			}
			var zero T
			return zero, fmt.Errorf("unknown enum choice %q", value)
		},
		Encode: func(value T) string {
			for _, choice := range choices {
				if choice.Value == value {
					return choice.ID
				}
			}
			return "<unknown>"
		},
		Canonical: func(value T) ([]byte, error) {
			for _, choice := range choices {
				if choice.Value == value {
					return []byte("enum:" + choice.ID), nil
				}
			}
			return nil, fmt.Errorf("enum value is not registered")
		},
		Validate: func(value T) []diagnostic.Item {
			for _, choice := range choices {
				if choice.Value == value {
					return nil
				}
			}
			return []diagnostic.Item{diagnostic.NewItem("config.enum-choice", diagnostic.ErrorSeverity, diagnostic.Path{}, "value is not a registered enum choice", nil)}
		},
		Description: Description{Type: "enum", Choices: enumDescriptions(choices)},
	})
	seenIDs := make(map[string]struct{}, len(choices))
	for _, choice := range choices {
		if choice.ID == "" || strings.TrimSpace(choice.ID) != choice.ID {
			result = result.addConstruction(diagnostic.NewItem("config.invalid-choice", diagnostic.ErrorSeverity, diagnostic.Path{}, "enum choice ID must be non-empty and trimmed", nil))
		}
		if _, exists := seenIDs[choice.ID]; exists {
			result = result.addConstruction(diagnostic.NewItem("config.duplicate-choice", diagnostic.ErrorSeverity, diagnostic.Path{}, "enum choice IDs must be unique", nil))
		}
		seenIDs[choice.ID] = struct{}{}
	}
	return result
}

// Optional represents an explicitly absent or present value.
type Optional[T any] struct {
	Value   T
	Present bool
}

// Some returns a present optional value.
func Some[T any](value T) Optional[T] { return Optional[T]{Value: value, Present: true} }

// None returns an absent optional value.
func None[T any]() Optional[T] { return Optional[T]{} }

// OptionalCodec adds an explicit "none" representation around a codec.
func OptionalCodec[T any](inner Codec[T]) Codec[Optional[T]] {
	result := NewCodec(CodecSpec[Optional[T]]{
		Type: "optional<" + inner.description.Type + ">",
		Decode: func(value string) (Optional[T], error) {
			if value == "none" || value == "null" {
				return None[T](), nil
			}
			decoded, err := inner.Decode(value)
			if err != nil {
				return Optional[T]{}, err
			}
			return Some(decoded), nil
		},
		Encode: func(value Optional[T]) string {
			if !value.Present {
				return "none"
			}
			return inner.Encode(value.Value)
		},
		Canonical: func(value Optional[T]) ([]byte, error) {
			if !value.Present {
				return []byte("optional:none"), nil
			}
			canonical, err := inner.Canonical(value.Value)
			if err != nil {
				return nil, err
			}
			return canonicalSequence("optional:value", canonical), nil
		},
		Clone: func(value Optional[T]) Optional[T] {
			if !value.Present {
				return None[T]()
			}
			return Some(inner.Clone(value.Value))
		},
		Normalize: func(value Optional[T]) (Optional[T], []diagnostic.Item) {
			if !value.Present {
				return value, nil
			}
			normalized, items := inner.normalizeValue(value.Value)
			value.Value = normalized
			return value, items
		},
		Validate: func(value Optional[T]) []diagnostic.Item {
			if !value.Present {
				return nil
			}
			return inner.validateValue(value.Value)
		},
		Description: func() Description {
			description := inner.descriptionValue()
			description.Type = "optional<" + description.Type + ">"
			description.Optional = true
			return description
		}(),
	})
	if !inner.Valid() {
		result = result.addConstruction(diagnostic.NewItem("config.invalid-optional-codec", diagnostic.ErrorSeverity, diagnostic.Path{}, "optional inner codec must be valid", nil))
	}
	return result
}

// AutoMode distinguishes an input-independent explicit value from a value
// that must be selected later from input context.
type AutoMode uint8

const (
	AutoModeAuto AutoMode = iota
	AutoModeValue
)

// Auto is an explicit sum type for input-dependent values. It is not a zero
// value sentinel and is resolved by Compile in a later milestone.
type Auto[T any] struct {
	Mode  AutoMode
	Value T
}

// AutoOf constructs an input-dependent value.
func AutoOf[T any]() Auto[T] { return Auto[T]{Mode: AutoModeAuto} }

// ValueOf constructs an explicit value.
func ValueOf[T any](value T) Auto[T] { return Auto[T]{Mode: AutoModeValue, Value: value} }

// AutoCodec wraps a value codec with an explicit "auto" choice.
func AutoCodec[T any](inner Codec[T]) Codec[Auto[T]] {
	result := NewCodec(CodecSpec[Auto[T]]{
		Type: "auto<" + inner.description.Type + ">",
		Decode: func(value string) (Auto[T], error) {
			if value == "auto" {
				return AutoOf[T](), nil
			}
			decoded, err := inner.Decode(value)
			if err != nil {
				return Auto[T]{}, err
			}
			return ValueOf(decoded), nil
		},
		Encode: func(value Auto[T]) string {
			if value.Mode == AutoModeAuto {
				return "auto"
			}
			return inner.Encode(value.Value)
		},
		Canonical: func(value Auto[T]) ([]byte, error) {
			if value.Mode == AutoModeAuto {
				return []byte("auto:auto"), nil
			}
			canonical, err := inner.Canonical(value.Value)
			if err != nil {
				return nil, err
			}
			return canonicalSequence("auto:value", canonical), nil
		},
		Clone: func(value Auto[T]) Auto[T] {
			if value.Mode == AutoModeAuto {
				return AutoOf[T]()
			}
			value.Value = inner.Clone(value.Value)
			return value
		},
		Normalize: func(value Auto[T]) (Auto[T], []diagnostic.Item) {
			if value.Mode == AutoModeAuto {
				return value, nil
			}
			normalized, items := inner.normalizeValue(value.Value)
			value.Value = normalized
			return value, items
		},
		Validate: func(value Auto[T]) []diagnostic.Item {
			if value.Mode == AutoModeAuto {
				return nil
			}
			return inner.validateValue(value.Value)
		},
		Description: func() Description {
			description := inner.descriptionValue()
			description.Type = "auto<" + description.Type + ">"
			description.Auto = true
			return description
		}(),
	})
	if !inner.Valid() {
		result = result.addConstruction(diagnostic.NewItem("config.invalid-auto-codec", diagnostic.ErrorSeverity, diagnostic.Path{}, "auto inner codec must be valid", nil))
	}
	return result
}

// RateMode distinguishes an explicitly requested rate from an input-derived
// rate. Rate is a concrete example of the Auto contract for common callers.
type RateMode uint8

const (
	RateModeAuto RateMode = iota
	RateModeFixed
)

// Rate is an explicit input-dependent sample rate request.
type Rate struct {
	Mode  RateMode
	Hertz int64
}

// AutoRate returns a rate selected by Compile.
func AutoRate() Rate { return Rate{Mode: RateModeAuto} }

// FixedRate returns an explicitly requested rate.
func FixedRate(hertz int64) Rate { return Rate{Mode: RateModeFixed, Hertz: hertz} }

// RateCodec returns the standard rate sum-type codec.
func RateCodec() Codec[Rate] {
	return NewCodec(CodecSpec[Rate]{
		Type: "rate",
		Decode: func(value string) (Rate, error) {
			if value == "auto" {
				return AutoRate(), nil
			}
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return Rate{}, fmt.Errorf("rate must be auto or an integer hertz value")
			}
			return FixedRate(parsed), nil
		},
		Encode: func(value Rate) string {
			if value.Mode == RateModeAuto {
				return "auto"
			}
			return strconv.FormatInt(value.Hertz, 10)
		},
		Canonical: func(value Rate) ([]byte, error) {
			if value.Mode == RateModeAuto {
				return []byte("rate:auto"), nil
			}
			return []byte("rate:" + strconv.FormatInt(value.Hertz, 10)), nil
		},
		Validate: func(value Rate) []diagnostic.Item {
			if value.Mode == RateModeAuto {
				return nil
			}
			if value.Hertz <= 0 {
				return []diagnostic.Item{diagnostic.NewItem("config.rate", diagnostic.ErrorSeverity, diagnostic.Path{}, "fixed rate must be positive", nil)}
			}
			return nil
		},
		Description: Description{Type: "rate", Unit: "Hz", Auto: true},
	})
}

// SecretValue stores a value whose display and canonical form are redacted.
// Reveal is an explicit capability used only by the component that needs the
// secret for execution.
type SecretValue[T any] struct {
	value T
}

// NewSecret constructs an opaque secret value.
func NewSecret[T any](value T) SecretValue[T] { return SecretValue[T]{value: value} }

// Reveal returns the secret to the caller that explicitly requested it.
func (value SecretValue[T]) Reveal() T { return value.value }

func (value SecretValue[T]) String() string { return "<redacted>" }

// SecretCodec wraps a codec and ensures raw values never enter human or
// canonical output. The fingerprint contains a domain-separated digest so
// equal secrets remain distinguishable without exposing the secret.
func SecretCodec[T any](inner Codec[T]) Codec[SecretValue[T]] {
	result := NewCodec(CodecSpec[SecretValue[T]]{
		Type: "secret<" + inner.description.Type + ">",
		Decode: func(value string) (SecretValue[T], error) {
			decoded, err := inner.Decode(value)
			if err != nil {
				return SecretValue[T]{}, fmt.Errorf("secret value is invalid")
			}
			return NewSecret(decoded), nil
		},
		Encode: func(SecretValue[T]) string { return "<redacted>" },
		Canonical: func(value SecretValue[T]) ([]byte, error) {
			canonical, err := inner.Canonical(value.value)
			if err != nil {
				return nil, fmt.Errorf("secret value cannot be canonicalized")
			}
			hash := sha256.New()
			hash.Write([]byte("godec/config/secret/v1\x00"))
			writeLength(hash, canonical)
			return hash.Sum(nil), nil
		},
		Clone: func(value SecretValue[T]) SecretValue[T] {
			return NewSecret(inner.Clone(value.value))
		},
		Normalize: func(value SecretValue[T]) (SecretValue[T], []diagnostic.Item) {
			normalized, _ := inner.normalizeValue(value.value)
			value.value = normalized
			return value, nil
		},
		Validate: func(value SecretValue[T]) []diagnostic.Item {
			if len(inner.validateValue(value.value)) != 0 {
				return []diagnostic.Item{diagnostic.NewItem("config.secret-invalid", diagnostic.ErrorSeverity, diagnostic.Path{}, "secret value is invalid", nil)}
			}
			return nil
		},
		Description: Description{Type: "secret<" + inner.description.Type + ">", Secret: true},
	})
	if !inner.Valid() {
		result = result.addConstruction(diagnostic.NewItem("config.invalid-secret-codec", diagnostic.ErrorSeverity, diagnostic.Path{}, "secret inner codec must be valid", nil))
	}
	return result
}

// Union is a discriminated union whose variants share the same Go value type.
type Union[T any] struct {
	Variant string
	Value   T
}

// UnionChoice describes one discriminated union variant.
type UnionChoice[T any] struct {
	ID    string
	Label string
	Codec Codec[T]
}

// UnionCodec returns a codec for values encoded as a JSON object with
// variant and value members.
func UnionCodec[T any](choices ...UnionChoice[T]) Codec[Union[T]] {
	byID := make(map[string]UnionChoice[T], len(choices))
	descriptions := make([]ChoiceDescription, 0, len(choices))
	result := NewCodec(CodecSpec[Union[T]]{
		Type: "union",
		Decode: func(value string) (Union[T], error) {
			var raw map[string]json.RawMessage
			if err := json.Unmarshal([]byte(value), &raw); err != nil || raw == nil {
				return Union[T]{}, fmt.Errorf("union value must be a JSON object")
			}
			encodedVariant, ok := raw["variant"]
			if !ok {
				return Union[T]{}, fmt.Errorf("union value must contain a variant")
			}
			var variant string
			if err := json.Unmarshal(encodedVariant, &variant); err != nil {
				return Union[T]{}, withDecodePath("variant", fmt.Errorf("union variant must be a JSON string"))
			}
			encoded, ok := raw["value"]
			if !ok {
				return Union[T]{}, fmt.Errorf("union value must contain a value")
			}
			for field := range raw {
				if field != "variant" && field != "value" {
					return Union[T]{}, withDecodePath(field, fmt.Errorf("unknown union field %q", field))
				}
			}
			choice, ok := byID[variant]
			if !ok {
				return Union[T]{}, fmt.Errorf("unknown union variant %q", variant)
			}
			decoded, err := choice.Codec.Decode(jsonValueText(encoded))
			if err != nil {
				return Union[T]{}, withDecodePath("value", err)
			}
			return Union[T]{Variant: variant, Value: decoded}, nil
		},
		Encode: func(value Union[T]) string {
			variant, err := json.Marshal(value.Variant)
			if err != nil {
				return "null"
			}
			encoded := "null"
			if choice, ok := byID[value.Variant]; ok {
				encoded = surfaceJSON(choice.Codec.Encode(value.Value))
			}
			return `{"variant":` + string(variant) + `,"value":` + encoded + "}"
		},
		Canonical: func(value Union[T]) ([]byte, error) {
			choice, ok := byID[value.Variant]
			if !ok {
				return nil, fmt.Errorf("union variant is not registered")
			}
			canonical, err := choice.Codec.Canonical(value.Value)
			if err != nil {
				return nil, err
			}
			return canonicalSequence("union:"+value.Variant, canonical), nil
		},
		Clone: func(value Union[T]) Union[T] {
			if choice, ok := byID[value.Variant]; ok {
				value.Value = choice.Codec.Clone(value.Value)
			}
			return value
		},
		Validate: func(value Union[T]) []diagnostic.Item {
			choice, ok := byID[value.Variant]
			if !ok {
				return []diagnostic.Item{diagnostic.NewItem("config.union-variant", diagnostic.ErrorSeverity, diagnostic.Path{}, "union variant is not registered", nil)}
			}
			return choice.Codec.validateValue(value.Value)
		},
		Description: Description{Type: "union", Choices: descriptions},
	})
	seen := make(map[string]struct{}, len(choices))
	for _, choice := range choices {
		if choice.ID == "" || !choice.Codec.Valid() {
			result = result.addConstruction(diagnostic.NewItem("config.invalid-union-variant", diagnostic.ErrorSeverity, diagnostic.Path{}, "union variants require an ID and codec", nil))
		}
		if _, exists := seen[choice.ID]; exists {
			result = result.addConstruction(diagnostic.NewItem("config.duplicate-union-variant", diagnostic.ErrorSeverity, diagnostic.Path{}, "union variant IDs must be unique", nil))
		}
		seen[choice.ID] = struct{}{}
		byID[choice.ID] = choice
		descriptions = append(descriptions, ChoiceDescription{ID: choice.ID, Label: choice.Label})
	}
	result.description.Choices = descriptions
	return result
}

// Slice returns an ordered slice codec. Element order is part of the
// canonical representation.
func Slice[T any](inner Codec[T]) Codec[[]T] {
	result := NewCodec(CodecSpec[[]T]{
		Type: "slice<" + inner.description.Type + ">",
		Decode: func(value string) ([]T, error) {
			if strings.TrimSpace(value) == "null" {
				return nil, nil
			}
			var raw []json.RawMessage
			if err := json.Unmarshal([]byte(value), &raw); err != nil {
				return nil, fmt.Errorf("slice must be a JSON array")
			}
			result := make([]T, len(raw))
			for index, item := range raw {
				decoded, err := inner.Decode(jsonValueText(item))
				if err != nil {
					return nil, withDecodePath(strconv.Itoa(index), fmt.Errorf("slice item %d: %w", index, err))
				}
				result[index] = decoded
			}
			return result, nil
		},
		Encode: func(value []T) string {
			if value == nil {
				return "null"
			}
			parts := make([]string, len(value))
			for index, item := range value {
				parts[index] = surfaceJSON(inner.Encode(item))
			}
			return "[" + strings.Join(parts, ",") + "]"
		},
		Canonical: func(value []T) ([]byte, error) {
			parts := make([][]byte, len(value))
			for index, item := range value {
				canonical, err := inner.Canonical(item)
				if err != nil {
					return nil, fmt.Errorf("slice item %d: %w", index, err)
				}
				parts[index] = canonical
			}
			return canonicalSequence("slice", parts...), nil
		},
		Clone: func(value []T) []T {
			if value == nil {
				return nil
			}
			result := make([]T, len(value))
			for index, item := range value {
				result[index] = inner.Clone(item)
			}
			return result
		},
		Normalize: func(value []T) ([]T, []diagnostic.Item) {
			result := make([]T, len(value))
			var items []diagnostic.Item
			for index, item := range value {
				normalized, childItems := inner.normalizeValue(item)
				result[index] = normalized
				for _, child := range childItems {
					child.Path = child.Path.Prefix(diagnostic.FieldPath(strconv.Itoa(index)))
					items = append(items, child)
				}
			}
			return result, items
		},
		Validate: func(value []T) []diagnostic.Item {
			var items []diagnostic.Item
			for index, item := range value {
				for _, child := range inner.validateValue(item) {
					child.Path = child.Path.Prefix(diagnostic.FieldPath(strconv.Itoa(index)))
					items = append(items, child)
				}
			}
			return items
		},
		Description: Description{Type: "slice<" + inner.description.Type + ">", Ordered: true},
	})
	if !inner.Valid() {
		result = result.addConstruction(diagnostic.NewItem("config.invalid-slice-codec", diagnostic.ErrorSeverity, diagnostic.Path{}, "slice inner codec must be valid", nil))
	}
	return result
}

// Map returns a map codec whose canonical entries are sorted by canonical key
// bytes. It is accepted only when both key and value codecs are canonical.
func Map[K comparable, V any](keyCodec Codec[K], valueCodec Codec[V]) Codec[map[K]V] {
	result := NewCodec(CodecSpec[map[K]V]{
		Type: "map<" + keyCodec.description.Type + "," + valueCodec.description.Type + ">",
		Decode: func(value string) (map[K]V, error) {
			if strings.TrimSpace(value) == "null" {
				return nil, nil
			}
			var raw map[string]json.RawMessage
			if err := json.Unmarshal([]byte(value), &raw); err != nil {
				return nil, fmt.Errorf("map must be a JSON object")
			}
			decoded := make(map[K]V, len(raw))
			for encodedKey, encodedValue := range raw {
				key, err := keyCodec.Decode(encodedKey)
				if err != nil {
					return nil, withDecodePath(encodedKey, fmt.Errorf("map key: %w", err))
				}
				item, err := valueCodec.Decode(jsonValueText(encodedValue))
				if err != nil {
					return nil, withDecodePath(encodedKey, fmt.Errorf("map value: %w", err))
				}
				decoded[key] = item
			}
			return decoded, nil
		},
		Encode: func(value map[K]V) string {
			if value == nil {
				return "null"
			}
			entries := sortedMapEntries(value, keyCodec)
			parts := make([]string, 0, len(entries))
			for _, entry := range entries {
				key, _ := json.Marshal(keyCodec.Encode(entry.key))
				parts = append(parts, string(key)+":"+surfaceJSON(valueCodec.Encode(entry.value)))
			}
			return "{" + strings.Join(parts, ",") + "}"
		},
		Canonical: func(value map[K]V) ([]byte, error) {
			entries := sortedMapEntries(value, keyCodec)
			parts := make([][]byte, 0, len(entries)*2)
			for _, entry := range entries {
				key, err := keyCodec.Canonical(entry.key)
				if err != nil {
					return nil, err
				}
				item, err := valueCodec.Canonical(entry.value)
				if err != nil {
					return nil, err
				}
				parts = append(parts, key, item)
			}
			return canonicalSequence("map", parts...), nil
		},
		Clone: func(value map[K]V) map[K]V {
			if value == nil {
				return nil
			}
			result := make(map[K]V, len(value))
			for key, item := range value {
				result[keyCodec.Clone(key)] = valueCodec.Clone(item)
			}
			return result
		},
		Normalize: func(value map[K]V) (map[K]V, []diagnostic.Item) {
			if value == nil {
				return nil, nil
			}
			result := make(map[K]V, len(value))
			var items []diagnostic.Item
			for key, item := range value {
				normalizedKey, keyItems := keyCodec.normalizeValue(key)
				normalizedValue, valueItems := valueCodec.normalizeValue(item)
				keyPath := keyCodec.Encode(key)
				for _, child := range keyItems {
					items = append(items, prefixItem(child, keyPath))
				}
				for _, child := range valueItems {
					items = append(items, prefixItem(child, keyPath))
				}
				if _, exists := result[normalizedKey]; exists {
					items = append(items, diagnostic.NewItem("config.map-key-collision", diagnostic.ErrorSeverity, diagnostic.FieldPath(keyPath), "map key normalization produced a duplicate key", nil))
					continue
				}
				result[normalizedKey] = normalizedValue
			}
			return result, items
		},
		Validate: func(value map[K]V) []diagnostic.Item {
			var items []diagnostic.Item
			for key, item := range value {
				for _, child := range keyCodec.validateValue(key) {
					items = append(items, prefixItem(child, keyCodec.Encode(key)))
				}
				for _, child := range valueCodec.validateValue(item) {
					items = append(items, prefixItem(child, keyCodec.Encode(key)))
				}
			}
			return items
		},
		Description: Description{Type: "map", Ordered: false},
	})
	if !keyCodec.Valid() || !valueCodec.Valid() {
		result = result.addConstruction(diagnostic.NewItem("config.invalid-map-codec", diagnostic.ErrorSeverity, diagnostic.Path{}, "map key and value codecs must be valid", nil))
	}
	return result
}

// Nested returns a codec backed by another typed schema. The nested schema's
// field IDs and canonical order define the nested representation.
func Nested[T any](schema Schema[T]) Codec[T] {
	result := NewCodec(CodecSpec[T]{
		Type:      "nested",
		Decode:    schema.decodeJSON,
		Encode:    schema.encodeJSON,
		Canonical: func(value T) ([]byte, error) { return schema.Canonical(value) },
		Clone: func(value T) T {
			cloned, _ := schema.snapshot(value)
			return cloned
		},
		Validate: func(value T) []diagnostic.Item {
			return schema.validateValue(value)
		},
		Description: Description{Type: "nested"},
	})
	if !schema.Valid() {
		for _, item := range schema.Diagnostics() {
			result = result.addConstruction(item)
		}
	}
	return result
}

type decodePathError struct {
	path []string
	err  error
}

func (e *decodePathError) Error() string { return e.err.Error() }

func (e *decodePathError) Unwrap() error { return e.err }

func withDecodePath(field string, err error) error {
	if err == nil {
		return nil
	}
	var pathErr *decodePathError
	if errors.As(err, &pathErr) {
		path := append([]string{field}, pathErr.path...)
		return &decodePathError{path: path, err: err}
	}
	return &decodePathError{path: []string{field}, err: err}
}

func decodePath(err error) []string {
	var pathErr *decodePathError
	if !errors.As(err, &pathErr) || pathErr == nil {
		return nil
	}
	return append([]string(nil), pathErr.path...)
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

func enumDescriptions[T comparable](choices []Choice[T]) []ChoiceDescription {
	descriptions := make([]ChoiceDescription, 0, len(choices))
	for _, choice := range choices {
		descriptions = append(descriptions, ChoiceDescription{ID: choice.ID, Label: choice.Label})
	}
	return descriptions
}

type mapEntry[K comparable, V any] struct {
	key   K
	value V
}

func sortedMapEntries[K comparable, V any](value map[K]V, codec Codec[K]) []mapEntry[K, V] {
	entries := make([]mapEntry[K, V], 0, len(value))
	for key, item := range value {
		entries = append(entries, mapEntry[K, V]{key: key, value: item})
	}
	sort.Slice(entries, func(left, right int) bool {
		leftCanonical, _ := codec.Canonical(entries[left].key)
		rightCanonical, _ := codec.Canonical(entries[right].key)
		return bytes.Compare(leftCanonical, rightCanonical) < 0
	})
	return entries
}

func canonicalSequence(tag string, values ...[]byte) []byte {
	result := []byte(tag)
	result = append(result, 0)
	for _, value := range values {
		result = appendLength(result, value)
	}
	return result
}

func appendLength(destination, value []byte) []byte {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	destination = append(destination, length[:]...)
	return append(destination, value...)
}

func writeLength(destination interface{ Write([]byte) (int, error) }, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = destination.Write(length[:])
	_, _ = destination.Write(value)
}

func jsonValueText(value []byte) string {
	var text string
	if len(value) != 0 && value[0] == '"' && json.Unmarshal(value, &text) == nil {
		return text
	}
	return string(value)
}
