package main

import (
	"fmt"
	"reflect"

	"github.com/godexture/sdk/optional"
)

type Config struct {
	Str optional.Optional[string]
	Int optional.Optional[int]
	B   bool
}

func ApplyDefaults[T any](cfg, def T) T {
	cfgVal := reflect.ValueOf(cfg)
	defVal := reflect.ValueOf(def)

	if cfgVal.Kind() != reflect.Struct {
		return cfg
	}

	out := reflect.New(cfgVal.Type()).Elem()
	for i := 0; i < cfgVal.NumField(); i++ {
		field := cfgVal.Field(i)
		defField := defVal.Field(i)

		// Check if it's an optional by looking for an Exists method
		existsMethod := field.MethodByName("Exists")
		if existsMethod.IsValid() {
			res := existsMethod.Call(nil)
			if len(res) > 0 && !res[0].Bool() {
				// Is empty optional, use default
				out.Field(i).Set(defField)
				continue
			}
		}

		// Just copy
		out.Field(i).Set(field)
	}

	return out.Interface().(T)
}

func main() {
	cfg := Config{
		Str: optional.Some("hello"),
	}
	def := Config{
		Str: optional.Some("default_str"),
		Int: optional.Some(42),
		B:   true,
	}

	res := ApplyDefaults(cfg, def)
	fmt.Printf("Str: %v, Int: %v, B: %v\n", res.Str.Unwrap(), res.Int.Unwrap(), res.B)
}
