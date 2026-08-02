package config

import "testing"

func BenchmarkSchemaResolve(b *testing.B) {
	schema := testSchema()
	patch := NewPatch().SetText("number", "8").SetText("values", "[1,2,3,5,8]")
	b.ReportAllocs()
	for b.Loop() {
		if _, err := schema.Resolve(patch); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCanonicalization(b *testing.B) {
	schema := testSchema()
	value := schema.Default()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := schema.Canonical(value); err != nil {
			b.Fatal(err)
		}
	}
}
