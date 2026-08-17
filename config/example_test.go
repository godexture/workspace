package config_test

import (
	"fmt"

	"github.com/godexture/godec/config"
)

type encoderConfig struct {
	Compression int
	Verify      bool
}

func encoderSchema() config.Schema[encoderConfig] {
	return config.Struct[encoderConfig](func() encoderConfig {
		return encoderConfig{Compression: 5}
	}).
		Version("1").
		AddField(config.Field(
			"compression",
			func(c *encoderConfig) *int { return &c.Compression },
			config.Int().Range(0, 8).Help("Compression effort"),
		)).
		AddField(config.Field(
			"verify",
			func(c *encoderConfig) *bool { return &c.Verify },
			config.Bool().Help("Verify encoded frames"),
		)).
		Preset("fast", func(c *encoderConfig) { c.Compression = 0 }).
		Build()
}

// A schema resolves a fresh default, one named preset, and a sparse patch into
// an immutable snapshot. Explicit values win over the preset.
func ExampleSchema_Resolve() {
	schema := encoderSchema()

	resolved, err := schema.Resolve(config.NewPatch().Preset("fast").SetText("verify", "true"))
	if err != nil {
		panic(err)
	}

	value, err := resolved.Value()
	if err != nil {
		panic(err)
	}
	compression, _ := resolved.Provenance().Source("compression")
	verify, _ := resolved.Provenance().Source("verify")
	fmt.Println(value.Compression, compression)
	fmt.Println(value.Verify, verify)
	// Output:
	// 0 preset
	// true explicit
}

// Rejected input reports the field path and the registered names it was
// closest to, rather than only stating that the input was unknown.
func ExampleSchema_Resolve_unknownField() {
	_, err := encoderSchema().Resolve(config.NewPatch().SetText("compresion", "3"))
	fmt.Println(err)
	// Output:
	// compresion: error: config.unknown-field: field is not registered by this schema; did you mean "compression"?
}

// Default returns an independent snapshot on every call, so a caller mutating
// one value cannot change what the next caller receives.
func ExampleSchema_Default() {
	schema := encoderSchema()

	first := schema.Default()
	first.Compression = 8

	fmt.Println(schema.Default().Compression)
	// Output: 5
}
