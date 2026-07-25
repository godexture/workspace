package cliflag

import (
	"fmt"
	"reflect"
	"slices"

	"github.com/spf13/pflag"
)

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
		field.flagName = name
		defaultValue := formatValue(value.Field(field.index))
		field.value = &flagValue{typeOf: field.typeOf, raw: defaultValue}
		flags.Var(field.value, name, helpWithDefault(field.help, defaultValue))
		// helpWithDefault already renders the actual default; suppress pflag's duplicate suffix.
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
		// helpWithDefault already renders the actual default; suppress pflag's duplicate suffix.
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
		flag := b.flags.Lookup(field.flagName)
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
		flag := b.flags.Lookup(field.flagName)
		if flag != nil && flag.Changed {
			names = append(names, flag.Name)
		}
	}
	if b.preset != nil {
		if flag := b.flags.Lookup(b.presetFlag); flag != nil && flag.Changed {
			names = append(names, b.presetFlag)
		}
	}
	return names
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
