package config

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/godexture/godec/diagnostic"
)

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

const redactionMarker = "<redacted>"

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
		Normalize: func(value Union[T]) (Union[T], []diagnostic.Item) {
			choice, ok := byID[value.Variant]
			if !ok {
				// Validate reports the unregistered variant; normalization has
				// no codec to delegate to.
				return value, nil
			}
			normalized, items := choice.Codec.normalizeValue(value.Value)
			value.Value = normalized
			result := make([]diagnostic.Item, 0, len(items))
			for _, item := range items {
				result = append(result, prefixItem(item, "value"))
			}
			return value, result
		},
		Validate: func(value Union[T]) []diagnostic.Item {
			choice, ok := byID[value.Variant]
			if !ok {
				return []diagnostic.Item{diagnostic.NewItem("config.union-variant", diagnostic.ErrorSeverity, diagnostic.Path{Fields: []string{"variant"}}, "union variant is not registered", nil)}
			}
			items := choice.Codec.validateValue(value.Value)
			result := make([]diagnostic.Item, 0, len(items))
			for _, item := range items {
				result = append(result, prefixItem(item, "value"))
			}
			return result
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

func enumDescriptions[T comparable](choices []Choice[T]) []ChoiceDescription {
	descriptions := make([]ChoiceDescription, 0, len(choices))
	for _, choice := range choices {
		descriptions = append(descriptions, ChoiceDescription{ID: choice.ID, Label: choice.Label})
	}
	return descriptions
}
