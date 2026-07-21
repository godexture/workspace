package cliflag

import (
	"encoding"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
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

func BindStruct(flags *pflag.FlagSet, namespace string, prototype any) (*Binding, error) {
	if flags == nil {
		return nil, fmt.Errorf("flag set must not be nil")
	}
	value, typeOf, err := structValue(prototype)
	if err != nil {
		return nil, err
	}
	fields, err := fieldsFor(typeOf)
	if err != nil {
		return nil, err
	}
	binding := &Binding{fields: fields, flags: flags, typeOf: typeOf}
	for index := range binding.fields {
		field := &binding.fields[index]
		name := field.name
		if namespace != "" {
			name = namespace + "." + name
		}
		if flags.Lookup(name) != nil {
			return nil, fmt.Errorf("flag %q already exists", name)
		}
		defaultValue := formatValue(value.Field(field.index))
		field.value = &flagValue{typeOf: field.typeOf, raw: defaultValue}
		flags.Var(field.value, name, helpWithDefault(field.help, defaultValue))
		flags.Lookup(name).DefValue = "0"
	}
	if _, ok := prototype.(presetApplier); ok {
		name := "preset"
		if namespace != "" {
			name = namespace + "." + name
		}
		if flags.Lookup(name) != nil {
			return nil, fmt.Errorf("flag %q already exists", name)
		}
		binding.preset = new(int)
		*binding.preset = -1
		binding.presetFlag = name
		flags.IntVar(binding.preset, name, -1, helpWithDefault("Configuration preset", "-1"))
		flags.Lookup(name).DefValue = "0"
	}
	return binding, nil
}

func helpWithDefault(help, defaultValue string) string {
	if help != "" {
		help += " "
	}
	return fmt.Sprintf("%s[%s]", help, defaultValue)
}

func (b *Binding) Apply(target any) error {
	value, typeOf, err := structValue(target)
	if err != nil {
		return err
	}
	if typeOf != b.typeOf {
		return fmt.Errorf("binding targets %s, got %s", b.typeOf, typeOf)
	}
	copy := reflect.New(typeOf).Elem()
	copy.Set(value)
	if b.preset != nil {
		flag := b.flags.Lookup(b.presetFlag)
		if flag != nil && flag.Changed {
			applier, ok := copy.Addr().Interface().(presetApplier)
			if !ok {
				return fmt.Errorf("--%s: configuration does not support presets", flag.Name)
			}
			applier.ApplyPreset(*b.preset)
		}
	}
	for _, field := range b.fields {
		flag := b.flags.Lookup(flagName(b.flags, field.value))
		if flag == nil || !flag.Changed {
			continue
		}
		if err := setField(copy.Field(field.index), field.value.values()); err != nil {
			return fmt.Errorf("--%s: %w", flag.Name, err)
		}
		if err := validateChoice(copy, field); err != nil {
			return fmt.Errorf("--%s: %w", flag.Name, err)
		}
	}
	if err := validate(copy); err != nil {
		return err
	}
	value.Set(copy)
	return nil
}

func (b *Binding) ChangedFlags() []string {
	names := make([]string, 0, len(b.fields))
	for _, field := range b.fields {
		flag := b.flags.Lookup(flagName(b.flags, field.value))
		if flag != nil && flag.Changed {
			names = append(names, flag.Name)
		}
	}
	if b.preset != nil && b.flags.Lookup(b.presetFlag).Changed {
		names = append(names, b.presetFlag)
	}
	return names
}

func DecodeStruct(target any, values map[string]string) error {
	value, typeOf, err := structValue(target)
	if err != nil {
		return err
	}
	fields, err := fieldsFor(typeOf)
	if err != nil {
		return err
	}
	byName := make(map[string]field, len(fields))
	for _, field := range fields {
		byName[field.name] = field
	}
	copy := reflect.New(typeOf).Elem()
	copy.Set(value)
	for name, raw := range values {
		field, ok := byName[name]
		if !ok {
			return fmt.Errorf("unknown configuration field %q", name)
		}
		if err := setField(copy.Field(field.index), []string{raw}); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if err := validateChoice(copy, field); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if err := validate(copy); err != nil {
		return err
	}
	value.Set(copy)
	return nil
}

func fieldsFor(typeOf reflect.Type) ([]field, error) {
	fields := make([]field, 0, typeOf.NumField())
	seen := map[string]struct{}{}
	for index := range typeOf.NumField() {
		structField := typeOf.Field(index)
		tag, exists := structField.Tag.Lookup("cli")
		if !exists || tag == "-" {
			continue
		}
		if !structField.IsExported() || tag == "" {
			return nil, fmt.Errorf("field %s has an invalid cli tag", structField.Name)
		}
		if !supportedType(structField.Type) {
			return nil, fmt.Errorf("field %s has unsupported CLI type %s", structField.Name, structField.Type)
		}
		if _, duplicate := seen[tag]; duplicate {
			return nil, fmt.Errorf("duplicate cli field %q", tag)
		}
		seen[tag] = struct{}{}
		fields = append(fields, field{index: index, goName: structField.Name, name: tag, help: structField.Tag.Get("help"), typeOf: structField.Type})
	}
	return fields, nil
}

func structValue(target any) (reflect.Value, reflect.Type, error) {
	value := reflect.ValueOf(target)
	if value.Kind() != reflect.Pointer || value.IsNil() || value.Elem().Kind() != reflect.Struct {
		return reflect.Value{}, nil, fmt.Errorf("configuration must be a non-nil pointer to a struct")
	}
	return value.Elem(), value.Elem().Type(), nil
}

func supportedType(typeOf reflect.Type) bool {
	if typeOf == reflect.TypeFor[time.Duration]() || typeOf.Kind() == reflect.Bool || typeOf.Kind() == reflect.String || typeOf.Kind() >= reflect.Int && typeOf.Kind() <= reflect.Float64 || typeOf.Kind() >= reflect.Uint && typeOf.Kind() <= reflect.Uint64 {
		return true
	}
	return typeOf.Kind() == reflect.Slice && typeOf.Elem().Kind() == reflect.String
}

func setField(destination reflect.Value, values []string) error {
	if destination.Kind() == reflect.Slice {
		result := reflect.MakeSlice(destination.Type(), len(values), len(values))
		for index, raw := range values {
			result.Index(index).SetString(raw)
		}
		destination.Set(result)
		return nil
	}
	if len(values) != 1 {
		return fmt.Errorf("accepts one value")
	}
	raw := values[0]
	if destination.CanAddr() {
		if unmarshaler, ok := destination.Addr().Interface().(encoding.TextUnmarshaler); ok {
			return unmarshaler.UnmarshalText([]byte(raw))
		}
	}
	switch destination.Kind() {
	case reflect.String:
		destination.SetString(raw)
	case reflect.Bool:
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return err
		}
		destination.SetBool(value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if destination.Type() == reflect.TypeFor[time.Duration]() {
			value, err := time.ParseDuration(raw)
			if err != nil {
				return err
			}
			destination.SetInt(int64(value))
			return nil
		}
		value, err := strconv.ParseInt(raw, 10, destination.Type().Bits())
		if err != nil {
			return err
		}
		destination.SetInt(value)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value, err := strconv.ParseUint(raw, 10, destination.Type().Bits())
		if err != nil {
			return err
		}
		destination.SetUint(value)
	case reflect.Float32, reflect.Float64:
		value, err := strconv.ParseFloat(raw, destination.Type().Bits())
		if err != nil {
			return err
		}
		destination.SetFloat(value)
	default:
		return fmt.Errorf("unsupported type %s", destination.Type())
	}
	return nil
}

func validate(value reflect.Value) error {
	if candidate, ok := value.Addr().Interface().(validator); ok {
		if err := candidate.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func validateChoice(value reflect.Value, field field) error {
	provider, ok := value.Addr().Interface().(choiceProvider)
	if !ok {
		return nil
	}
	choices := provider.FieldChoices(field.goName)
	if len(choices) == 0 {
		return nil
	}
	actual := fmt.Sprint(value.Field(field.index).Interface())
	if !slices.Contains(choices, actual) {
		return fmt.Errorf("must be one of %s", strings.Join(choices, ", "))
	}
	return nil
}

func formatValue(value reflect.Value) string {
	if value.Kind() == reflect.Slice {
		items := make([]string, value.Len())
		for index := range value.Len() {
			items[index] = value.Index(index).String()
		}
		return strings.Join(items, ",")
	}
	return fmt.Sprint(value.Interface())
}

type flagValue struct {
	typeOf   reflect.Type
	raw      string
	repeated []string
}

func (v *flagValue) Set(raw string) error {
	if v.typeOf.Kind() == reflect.Slice {
		v.repeated = append(v.repeated, raw)
	} else {
		v.raw = raw
	}
	return nil
}
func (v *flagValue) String() string { return v.raw }
func (v *flagValue) Type() string   { return v.typeOf.String() }
func (v *flagValue) values() []string {
	if v.typeOf.Kind() == reflect.Slice {
		return slices.Clone(v.repeated)
	}
	return []string{v.raw}
}

func flagName(flags *pflag.FlagSet, value *flagValue) string {
	var result string
	flags.VisitAll(func(flag *pflag.Flag) {
		if flag.Value == value {
			result = flag.Name
		}
	})
	return result
}
