package config

import "github.com/godexture/godec/diagnostic"

type nestedConfig struct {
	Limit int
}

type testConfig struct {
	Number int
	Verify bool
	Values []int
	Labels map[string]int
	Nested nestedConfig
	Secret SecretValue[string]
	Rate   Rate
}

func testSchema() Schema[testConfig] {
	builder := Struct[testConfig](func() testConfig {
		return defaultTestConfig()
	}).Version("1")

	builder.AddField(Field("number", func(value *testConfig) *int { return &value.Number }, Int().Range(0, 10).Help("number")))
	builder.AddField(Field("verify", func(value *testConfig) *bool { return &value.Verify }, Bool()))
	builder.AddField(Field("values", func(value *testConfig) *[]int { return &value.Values }, Slice(Int())))
	builder.AddField(Field("labels", func(value *testConfig) *map[string]int { return &value.Labels }, Map(String(), Int())))
	builder.AddField(Field("nested", func(value *testConfig) *nestedConfig { return &value.Nested }, Nested(testNestedSchema())))
	builder.AddField(Field("secret", func(value *testConfig) *SecretValue[string] { return &value.Secret }, SecretCodec(String())))
	builder.AddField(Field("rate", func(value *testConfig) *Rate { return &value.Rate }, RateCodec()))
	return builder.Build()
}

func testNestedSchema() Schema[nestedConfig] {
	return Struct[nestedConfig](func() nestedConfig { return nestedConfig{Limit: 3} }).
		Version("1").
		AddField(Field("limit", func(value *nestedConfig) *int { return &value.Limit }, Int())).
		Build()
}

func defaultTestConfig() testConfig {
	return testConfig{
		Number: 5,
		Values: []int{1, 2},
		Labels: map[string]int{"b": 2, "a": 1},
		Nested: nestedConfig{Limit: 3},
		Secret: NewSecret("default-token"),
		Rate:   AutoRate(),
	}
}

func diagnosticItems(err error) []diagnostic.Item { return diagnostic.ItemsOf(err) }
