package config

import (
	"strings"
	"testing"
)

func TestUnknownFieldAndPresetSuggestAlternatives(t *testing.T) {
	schema := Struct[testConfig](func() testConfig { return defaultTestConfig() }).Version("1").
		AddField(Field("number", func(value *testConfig) *int { return &value.Number }, Int(), Alias("num"))).
		AddField(Field("verify", func(value *testConfig) *bool { return &value.Verify }, Bool())).
		AddField(Field("values", func(value *testConfig) *[]int { return &value.Values }, Slice(Int()))).
		AddField(Field("labels", func(value *testConfig) *map[string]int { return &value.Labels }, Map(String(), Int()))).
		AddField(Field("nested", func(value *testConfig) *nestedConfig { return &value.Nested }, Nested(testNestedSchema()))).
		AddField(Field("secret", func(value *testConfig) *SecretValue[string] { return &value.Secret }, SecretCodec(String()))).
		AddField(Field("rate", func(value *testConfig) *Rate { return &value.Rate }, RateCodec())).
		Preset("fast", func(value *testConfig) { value.Number = 1 }).
		Build()
	if err := schema.Err(); err != nil {
		t.Fatalf("schema: %v", err)
	}

	_, err := schema.Resolve(NewPatch().SetText("numbre", "1"))
	if err == nil || !strings.Contains(err.Error(), `"number"`) {
		t.Fatalf("unknown field did not suggest the registered ID: %v", err)
	}
	_, err = schema.Resolve(NewPatch().Preset("fastest"))
	if err == nil || !strings.Contains(err.Error(), `"fast"`) {
		t.Fatalf("unknown preset did not suggest the registered preset: %v", err)
	}
	for _, item := range diagnosticItems(err) {
		if item.Code == codeUnknownPreset && item.Path.String() == "" {
			t.Fatalf("unknown preset diagnostic has no path: %#v", item)
		}
	}
	if _, err := schema.Resolve(NewPatch().SetText("totally-unrelated-xyz", "1")); err == nil || strings.Contains(err.Error(), "did you mean") {
		t.Fatalf("unrelated input produced a suggestion: %v", err)
	}
}
