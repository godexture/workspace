package cliflag

import (
	"reflect"
	"slices"
	"time"

	"github.com/spf13/pflag"
)

type Binding struct {
	fields     []field
	flags      *pflag.FlagSet
	typeOf     reflect.Type
	preset     *int
	presetFlag string
}

type FieldDescription struct {
	Name    string
	Type    string
	Help    string
	Default string
	Choices []string
}

type FieldValue struct {
	Name  string
	Value string
}

func StructValues(target any) ([]FieldValue, error) {
	value, typeOf, err := structValue(target)
	if err != nil {
		return nil, err
	}
	fields, err := fieldsFor(typeOf)
	if err != nil {
		return nil, err
	}
	values := make([]FieldValue, 0, len(fields))
	for _, field := range fields {
		values = append(values, FieldValue{Name: field.name, Value: descriptionDefault(value.Field(field.index))})
	}
	return values, nil
}

type field struct {
	index  int
	goName string
	name   string
	help   string
	typeOf reflect.Type
	value  *flagValue
}

type choiceProvider interface {
	FieldChoices(string) []string
}

type validator interface {
	Validate() error
}

type presetApplier interface {
	ApplyPreset(int)
}

func DescribeStruct(prototype any) ([]FieldDescription, error) {
	value, typeOf, err := structValue(prototype)
	if err != nil {
		return nil, err
	}
	fields, err := fieldsFor(typeOf)
	if err != nil {
		return nil, err
	}
	provider, _ := prototype.(choiceProvider)
	descriptions := make([]FieldDescription, 0, len(fields)+1)
	if _, ok := prototype.(presetApplier); ok {
		descriptions = append(descriptions, FieldDescription{
			Name: "preset", Type: "int", Help: "Configuration preset", Default: "-1",
		})
	}
	for _, field := range fields {
		var choices []string
		if provider != nil {
			choices = slices.Clone(provider.FieldChoices(field.goName))
		}
		descriptions = append(descriptions, FieldDescription{
			Name:    field.name,
			Type:    descriptionType(field.typeOf, choices),
			Help:    field.help,
			Default: descriptionDefault(value.Field(field.index)),
			Choices: choices,
		})
	}
	return descriptions, nil
}

func descriptionDefault(value reflect.Value) string {
	if value.Kind() == reflect.Slice && value.Len() == 0 {
		return "[]"
	}
	if value.Kind() == reflect.String && value.Len() == 0 {
		return `""`
	}
	return formatValue(value)
}

func descriptionType(typeOf reflect.Type, choices []string) string {
	if len(choices) > 0 {
		return "enum"
	}
	if typeOf == reflect.TypeFor[time.Duration]() {
		return "duration"
	}
	switch typeOf.Kind() {
	case reflect.Slice:
		return typeOf.Elem().Kind().String() + "..."
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return typeOf.Kind().String()
	default:
		return typeOf.String()
	}
}
