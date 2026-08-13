package config

import (
	"fmt"
	"strings"
	"testing"

	"github.com/godexture/godec/diagnostic"
)

// panicSecret is the value every panicking callback carries. No diagnostic,
// error, or rendered patch may contain it.
const panicSecret = "callback-panic-secret"

type panicConfig struct {
	Value string
}

type panicCodecSpec struct {
	decode    bool
	encode    bool
	canonical bool
	clone     bool
	normalize bool
	validate  bool
}

func panicCodec(spec panicCodecSpec) Codec[string] {
	return NewCodec(CodecSpec[string]{
		Type: "panic",
		Decode: func(value string) (string, error) {
			if spec.decode {
				panic(panicSecret)
			}
			return value, nil
		},
		Encode: func(value string) string {
			if spec.encode {
				panic(panicSecret)
			}
			return value
		},
		Canonical: func(value string) ([]byte, error) {
			if spec.canonical {
				panic(panicSecret)
			}
			return []byte("panic:" + value), nil
		},
		Clone: func(value string) string {
			if spec.clone {
				panic(panicSecret)
			}
			return value
		},
		Normalize: func(value string) (string, []diagnostic.Item) {
			if spec.normalize {
				panic(panicSecret)
			}
			return value, nil
		},
		Validate: func(string) []diagnostic.Item {
			if spec.validate {
				panic(panicSecret)
			}
			return nil
		},
	})
}

func panicSchema(spec panicCodecSpec) Schema[panicConfig] {
	return Struct[panicConfig](func() panicConfig { return panicConfig{Value: "ok"} }).
		Version("1").
		AddField(Field("value", func(value *panicConfig) *string { return &value.Value }, panicCodec(spec))).
		Build()
}

func TestCallbackPanicBecomesDiagnostic(t *testing.T) {
	cases := []struct {
		name    string
		resolve func() (Resolved[panicConfig], error)
	}{
		{"clone", func() (Resolved[panicConfig], error) {
			return panicSchema(panicCodecSpec{clone: true}).Resolve(NewPatch())
		}},
		{"normalize", func() (Resolved[panicConfig], error) {
			return panicSchema(panicCodecSpec{normalize: true}).Resolve(NewPatch())
		}},
		{"validate", func() (Resolved[panicConfig], error) {
			return panicSchema(panicCodecSpec{validate: true}).Resolve(NewPatch())
		}},
		{"canonical", func() (Resolved[panicConfig], error) {
			return panicSchema(panicCodecSpec{canonical: true}).Resolve(NewPatch())
		}},
		{"decode", func() (Resolved[panicConfig], error) {
			return panicSchema(panicCodecSpec{decode: true}).Resolve(NewPatch().SetText("value", "input"))
		}},
		{"accessor", func() (Resolved[panicConfig], error) {
			schema := Struct[panicConfig](func() panicConfig { return panicConfig{Value: "ok"} }).
				Version("1").
				AddField(Field("value", func(*panicConfig) *string { panic(panicSecret) }, String())).
				Build()
			return schema.Resolve(NewPatch())
		}},
		{"default-factory", func() (Resolved[panicConfig], error) {
			schema := Struct[panicConfig](func() panicConfig { panic(panicSecret) }).
				Version("1").
				AddField(Field("value", func(value *panicConfig) *string { return &value.Value }, String())).
				Build()
			return schema.Resolve(NewPatch())
		}},
		{"preset", func() (Resolved[panicConfig], error) {
			schema := Struct[panicConfig](func() panicConfig { return panicConfig{Value: "ok"} }).
				Version("1").
				AddField(Field("value", func(value *panicConfig) *string { return &value.Value }, String())).
				Preset("broken", func(*panicConfig) { panic(panicSecret) }).
				Build()
			return schema.Resolve(NewPatch().Preset("broken"))
		}},
		{"schema-validator", func() (Resolved[panicConfig], error) {
			schema := Struct[panicConfig](func() panicConfig { return panicConfig{Value: "ok"} }).
				Version("1").
				AddField(Field("value", func(value *panicConfig) *string { return &value.Value }, String())).
				Validate(func(panicConfig) []diagnostic.Item { panic(panicSecret) }).
				Build()
			return schema.Resolve(NewPatch())
		}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			resolved, err := testCase.resolve()
			if err == nil {
				t.Fatal("panicking callback resolved without an error")
			}
			assertNoPanicSecret(t, err.Error())
			for _, item := range resolved.Diagnostics() {
				assertNoPanicSecret(t, item.Message)
				for key, value := range item.Detail {
					assertNoPanicSecret(t, key+"="+value)
				}
			}
			if resolved.Fingerprint() != (Fingerprint{}) {
				t.Error("a panicking callback produced a fingerprint")
			}
		})
	}
}

// A panic during Build must be reported as a construction diagnostic, not
// escape through package initialization or Host construction.
func TestCallbackPanicDuringBuild(t *testing.T) {
	schema := panicSchema(panicCodecSpec{validate: true})
	if schema.Valid() {
		t.Fatal("schema with a panicking validator reported valid")
	}
	if !hasCode(schema.Diagnostics(), codeCallbackPanic) {
		t.Errorf("construction diagnostics %v have no %s item", schema.Diagnostics(), codeCallbackPanic)
	}
	assertNoPanicSecret(t, schema.Err().Error())
}

// Display-only operations have no failure channel, so they degrade to the
// invalid marker rather than unwinding through a surface renderer.
func TestCallbackPanicInDisplayOperations(t *testing.T) {
	codec := panicCodec(panicCodecSpec{encode: true})
	if encoded := codec.Encode("value"); encoded != invalidText {
		t.Errorf("panicking Encode returned %q, want %q", encoded, invalidText)
	}
	schema := panicSchema(panicCodecSpec{encode: true})
	view, err := schema.View().Resolve(NewPatch())
	if err != nil {
		t.Fatalf("resolve with panicking Encode failed: %v", err)
	}
	for _, field := range view.Summary().Fields() {
		if field.Value != invalidText {
			t.Errorf("summary field %s rendered %q, want %q", field.ID, field.Value, invalidText)
		}
	}
}

// Decode reports its own panic so a caller outside the schema pipeline still
// receives an error instead of unwinding.
func TestCallbackPanicInDirectDecode(t *testing.T) {
	if _, err := panicCodec(panicCodecSpec{decode: true}).Decode("input"); err == nil {
		t.Fatal("panicking Decode returned no error")
	} else {
		assertNoPanicSecret(t, err.Error())
	}
	if _, err := panicCodec(panicCodecSpec{canonical: true}).Canonical("input"); err == nil {
		t.Fatal("panicking Canonical returned no error")
	} else {
		assertNoPanicSecret(t, err.Error())
	}
}

func hasCode(items []diagnostic.Item, code string) bool {
	for _, item := range items {
		if item.Code == code {
			return true
		}
	}
	return false
}

func assertNoPanicSecret(t *testing.T, value string) {
	t.Helper()
	if strings.Contains(value, panicSecret) {
		t.Errorf("recovered panic value leaked into %q", value)
	}
	if strings.Contains(fmt.Sprintf("%q", value), panicSecret) {
		t.Errorf("recovered panic value leaked into quoted %q", value)
	}
}
