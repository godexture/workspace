package config

import (
	"fmt"
	"reflect"

	"github.com/godexture/godec/diagnostic"
)

// FieldOption changes a field's read-only description and dependency metadata.
type FieldOption func(*fieldOptions)

type fieldOptions struct {
	description Description
	aliases     []string
	dependsOn   []string
}

// Help sets a field help string.
func Help(value string) FieldOption {
	return func(options *fieldOptions) { options.description.Help = value }
}

// Unit sets a field display unit.
func Unit(value string) FieldOption {
	return func(options *fieldOptions) { options.description.Unit = value }
}

// Alias adds surface aliases. Aliases are searchable names, not identities.
func Alias(values ...string) FieldOption {
	return func(options *fieldOptions) { options.aliases = append(options.aliases, values...) }
}

// DependsOn declares control-plane field dependencies for surface projection.
func DependsOn(values ...string) FieldOption {
	return func(options *fieldOptions) { options.dependsOn = append(options.dependsOn, values...) }
}

// FieldSpec is a typed field registration produced by Field and consumed by
// Builder.AddField. The generic accessor makes Go detect field type changes.
type FieldSpec[C any] struct {
	id           string
	valueType    reflect.Type
	description  Description
	aliases      []string
	dependsOn    []string
	read         func(*C) (any, error)
	write        func(*C, any) error
	target       func(*C) uintptr
	encode       func(any) string
	decode       func(string) (any, error)
	canonical    func(any) ([]byte, error)
	normalize    func(any) (any, []diagnostic.Item)
	validate     func(any) []diagnostic.Item
	construction []diagnostic.Item
}

// Field registers a typed accessor and codec. It never panics for an invalid
// codec or accessor; the builder stores the problem for aggregate reporting.
func Field[C any, T any](id string, accessor func(*C) *T, codec Codec[T], options ...FieldOption) FieldSpec[C] {
	fieldOptionsValue := fieldOptions{description: codec.descriptionValue()}
	for _, option := range options {
		if option != nil {
			option(&fieldOptionsValue)
		}
	}
	result := FieldSpec[C]{
		id:          id,
		valueType:   reflect.TypeFor[T](),
		description: fieldOptionsValue.description,
		aliases:     append([]string(nil), fieldOptionsValue.aliases...),
		dependsOn:   append([]string(nil), fieldOptionsValue.dependsOn...),
		canonical: func(value any) ([]byte, error) {
			typed, ok := value.(T)
			if !ok {
				return nil, fmt.Errorf("field value has type %T, want %s", value, reflect.TypeFor[T]())
			}
			return codec.Canonical(typed)
		},
		encode: func(value any) string {
			typed, ok := value.(T)
			if !ok {
				return invalidText
			}
			return codec.Encode(typed)
		},
		decode: func(value string) (cloned any, err error) {
			decoded, err := codec.Decode(value)
			if err != nil {
				return nil, err
			}
			defer guardError(operationDecode, &err)
			return codec.Clone(decoded), nil
		},
		normalize: func(value any) (any, []diagnostic.Item) {
			typed, ok := value.(T)
			if !ok {
				return value, []diagnostic.Item{diagnostic.NewItem(codeTypeMismatch, diagnostic.ErrorSeverity, diagnostic.Path{}, "field value has the wrong type", nil)}
			}
			return guardNormalize(operationNormalize, diagnostic.Path{}, value, func() (any, []diagnostic.Item) {
				normalized, items := codec.normalizeValue(typed)
				return codec.Clone(normalized), items
			})
		},
		validate: func(value any) []diagnostic.Item {
			typed, ok := value.(T)
			if !ok {
				return []diagnostic.Item{diagnostic.NewItem(codeTypeMismatch, diagnostic.ErrorSeverity, diagnostic.Path{}, "field value has the wrong type", nil)}
			}
			return codec.validateValue(typed)
		},
		construction: codec.constructionItems(),
	}
	if accessor == nil {
		result.construction = append(result.construction, diagnostic.NewItem(codeMissingAccessor, diagnostic.ErrorSeverity, diagnostic.Path{}, "field accessor is required", nil))
	}
	if !codec.Valid() {
		result.construction = append(result.construction, diagnostic.NewItem(codeMissingCodec, diagnostic.ErrorSeverity, diagnostic.Path{}, "field codec must provide decode, encode, canonical, clone, normalize, and validation operations", nil))
	}
	result.read = func(config *C) (value any, err error) {
		if accessor == nil {
			return nil, fmt.Errorf("field accessor is missing")
		}
		defer guardError(operationRead, &err)
		target := accessor(config)
		if target == nil {
			return nil, fmt.Errorf("field accessor returned nil")
		}
		return codec.Clone(*target), nil
	}
	result.write = func(config *C, value any) (err error) {
		if accessor == nil {
			return fmt.Errorf("field accessor is missing")
		}
		typed, ok := value.(T)
		if !ok {
			return fmt.Errorf("field value has type %T, want %s", value, reflect.TypeFor[T]())
		}
		defer guardError(operationWrite, &err)
		target := accessor(config)
		if target == nil {
			return fmt.Errorf("field accessor returned nil")
		}
		*target = codec.Clone(typed)
		return nil
	}
	result.target = func(config *C) uintptr {
		defer func() {
			if recover() != nil {
				// An invalid accessor is reported by its normal read/write path.
			}
		}()
		if accessor == nil || config == nil {
			return 0
		}
		value := accessor(config)
		if value == nil {
			return 0
		}
		return reflect.ValueOf(value).Pointer()
	}
	return result
}

func cloneFieldSpec[C any](field FieldSpec[C]) FieldSpec[C] {
	field.aliases = append([]string(nil), field.aliases...)
	field.dependsOn = append([]string(nil), field.dependsOn...)
	field.description = cloneDescription(field.description)
	field.construction = cloneItems(field.construction)
	return field
}

func (field FieldSpec[C]) descriptionValue() FieldDescription {
	description := cloneDescription(field.description)
	return FieldDescription{
		ID:        field.id,
		Type:      description.Type,
		Help:      description.Help,
		Unit:      description.Unit,
		Secret:    description.Secret,
		Optional:  description.Optional,
		Auto:      description.Auto,
		Ordered:   description.Ordered,
		Range:     description.Range,
		Choices:   description.Choices,
		Aliases:   append([]string(nil), field.aliases...),
		DependsOn: append([]string(nil), field.dependsOn...),
	}
}

func inputDetail[C any](field FieldSpec[C]) map[string]string {
	detail := map[string]string{
		"source":   "patch",
		"expected": field.description.Type,
	}
	if field.description.Range != nil {
		detail["constraint"] = "range"
	}
	return detail
}
