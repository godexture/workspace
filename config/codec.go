package config

import (
	"encoding/binary"
	"fmt"
	"math"
	"reflect"
	"strconv"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/internal/snapshot"
)

// CodecSpec describes a field codec. Canonical is required: a schema cannot
// be registered for a type that has no deterministic fingerprint encoding.
// Callers must redact values in custom diagnostics; secret handling is
// provided by SecretCodec for values that should never be rendered.
type CodecSpec[T any] struct {
	Type      string
	Decode    func(string) (T, error)
	Encode    func(T) string
	Canonical func(T) ([]byte, error)
	// Clone is required for values containing mutable reference state.
	Clone       func(T) T
	Normalize   func(T) (T, []diagnostic.Item)
	Validate    func(T) []diagnostic.Item
	Description Description
}

// Codec is a typed control-plane codec. Its value methods return a copy so
// field declarations can be composed without mutable package state.
type Codec[T any] struct {
	decode       func(string) (T, error)
	encode       func(T) string
	canonical    func(T) ([]byte, error)
	clone        func(T) T
	normalize    func(T) (T, []diagnostic.Item)
	validate     func(T) []diagnostic.Item
	description  Description
	construction []diagnostic.Item
}

// NewCodec constructs a custom codec without requiring changes to this
// package. A missing canonical function is reported when the codec is used in
// a schema, rather than panicking during package initialization.
func NewCodec[T any](spec CodecSpec[T]) Codec[T] {
	if spec.Type == "" {
		spec.Type = reflect.TypeFor[T]().String()
	}
	if spec.Encode == nil {
		spec.Encode = func(value T) string { return fmt.Sprint(value) }
	}
	var construction []diagnostic.Item
	if spec.Clone == nil {
		spec.Clone = func(value T) T { return value }
		if snapshot.NeedsClone(reflect.TypeFor[T]()) {
			construction = append(construction, diagnostic.NewItem(codeMissingClone, diagnostic.ErrorSeverity, diagnostic.Path{}, "reference-valued codec must provide a Clone operation", nil))
		}
	}
	if spec.Normalize == nil {
		spec.Normalize = func(value T) (T, []diagnostic.Item) { return value, nil }
	}
	if spec.Validate == nil {
		spec.Validate = func(T) []diagnostic.Item { return nil }
	}
	spec.Description.Type = spec.Type
	return Codec[T]{
		decode:       spec.Decode,
		encode:       spec.Encode,
		canonical:    spec.Canonical,
		clone:        spec.Clone,
		normalize:    spec.Normalize,
		validate:     spec.Validate,
		description:  cloneDescription(spec.Description),
		construction: construction,
	}
}

// Valid reports whether the codec has the operations required for schema
// registration.
func (c Codec[T]) Valid() bool {
	return c.decode != nil && c.encode != nil && c.canonical != nil && c.clone != nil && c.normalize != nil && c.validate != nil && len(c.construction) == 0
}

// Decode parses one surface value.
func (c Codec[T]) Decode(value string) (decoded T, err error) {
	if c.decode == nil {
		var zero T
		return zero, fmt.Errorf("config codec %s has no decoder", c.description.Type)
	}
	defer guardError(operationDecode, &err)
	return c.decode(value)
}

// Encode renders a value for human-facing display. It is not a wire
// representation and is not used for fingerprints.
func (c Codec[T]) Encode(value T) string {
	if c.encode == nil {
		return invalidText
	}
	return guardValue(invalidText, func() string { return c.encode(value) })
}

// Canonical returns the deterministic field encoding used for fingerprints.
func (c Codec[T]) Canonical(value T) (canonical []byte, err error) {
	if c.canonical == nil {
		return nil, fmt.Errorf("config codec %s has no canonical encoder", c.description.Type)
	}
	defer guardError(operationCanonical, &err)
	return c.canonical(value)
}

// Clone returns a defensive copy of a field value. A panic in a declared clone
// is caught by the field boundary that asked for the snapshot, which is the
// only place that knows which field to blame.
func (c Codec[T]) Clone(value T) T {
	if c.clone == nil {
		return value
	}
	return c.clone(value)
}

func (c Codec[T]) normalizeValue(value T) (T, []diagnostic.Item) {
	if c.normalize == nil {
		return value, nil
	}
	return guardNormalize(operationNormalize, diagnostic.Path{}, value, func() (T, []diagnostic.Item) { return c.normalize(value) })
}

func (c Codec[T]) validateValue(value T) []diagnostic.Item {
	if c.validate == nil {
		return nil
	}
	return guardItems(operationValidate, diagnostic.Path{}, func() []diagnostic.Item { return c.validate(value) })
}

func (c Codec[T]) descriptionValue() Description {
	return cloneDescription(c.description)
}

func (c Codec[T]) constructionItems() []diagnostic.Item {
	items := make([]diagnostic.Item, len(c.construction))
	copy(items, c.construction)
	return items
}

// Help sets the human-facing help text.
func (c Codec[T]) Help(help string) Codec[T] {
	c.description.Help = help
	return c
}

// Unit declares a display unit without changing validation or canonical form.
func (c Codec[T]) Unit(unit string) Codec[T] {
	c.description.Unit = unit
	return c
}

// Range adds an inclusive numeric range check and description. It is valid
// for integer and floating-point codecs, including named numeric types.
func (c Codec[T]) Range(min, max T) Codec[T] {
	minValue, minOK := orderedFloat(min)
	maxValue, maxOK := orderedFloat(max)
	if !minOK || !maxOK || !finite(minValue) || !finite(maxValue) || maxValue <= minValue {
		return c.addConstruction(diagnostic.NewItem("config.invalid-range", diagnostic.ErrorSeverity, diagnostic.Path{}, "range must be finite and have max greater than min", nil))
	}
	c.description.Range = &Range{Min: minValue, Max: maxValue}
	previous := c.validate
	c.validate = func(value T) []diagnostic.Item {
		items := callValidate(previous, value)
		actual, ok := orderedFloat(value)
		if !ok || actual < minValue || actual > maxValue {
			items = append(items, diagnostic.NewItem("config.range", diagnostic.ErrorSeverity, diagnostic.Path{}, "value is outside the allowed range", map[string]string{
				"rule":     "range",
				"expected": fmt.Sprintf("%s..%s", formatNumber(minValue), formatNumber(maxValue)),
			}))
		}
		return items
	}
	return c
}

// Step adds an inclusive numeric step check relative to the range minimum.
func (c Codec[T]) Step(step T) Codec[T] {
	stepValue, ok := orderedFloat(step)
	if !ok || !finite(stepValue) || stepValue <= 0 {
		return c.addConstruction(diagnostic.NewItem("config.invalid-step", diagnostic.ErrorSeverity, diagnostic.Path{}, "step must be finite and positive", nil))
	}
	if c.description.Range == nil {
		return c.addConstruction(diagnostic.NewItem("config.step-without-range", diagnostic.ErrorSeverity, diagnostic.Path{}, "step requires a range", nil))
	}
	c.description.Range.Step = stepValue
	minimum := c.description.Range.Min
	previous := c.validate
	c.validate = func(value T) []diagnostic.Item {
		items := callValidate(previous, value)
		actual, valid := orderedFloat(value)
		if valid {
			steps := (actual - minimum) / stepValue
			if math.Abs(steps-math.Round(steps)) > 1e-9 {
				items = append(items, diagnostic.NewItem("config.step", diagnostic.ErrorSeverity, diagnostic.Path{}, "value does not match the declared step", map[string]string{
					"rule": "step",
				}))
			}
		}
		return items
	}
	return c
}

func (c Codec[T]) addConstruction(item diagnostic.Item) Codec[T] {
	c.construction = append(append([]diagnostic.Item(nil), c.construction...), item)
	return c
}

func callValidate[T any](validate func(T) []diagnostic.Item, value T) []diagnostic.Item {
	if validate == nil {
		return nil
	}
	return append([]diagnostic.Item(nil), validate(value)...)
}

func orderedFloat[T any](value T) (float64, bool) {
	reflected := reflect.ValueOf(value)
	if !reflected.IsValid() {
		return 0, false
	}
	switch reflected.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(reflected.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return float64(reflected.Uint()), true
	case reflect.Float32, reflect.Float64:
		return reflected.Float(), true
	default:
		return 0, false
	}
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func formatNumber(value float64) string {
	return strconv.FormatFloat(value, 'g', -1, 64)
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
