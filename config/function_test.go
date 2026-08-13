package config

import "testing"

// A function value cannot be canonicalized, so a config type that stores one
// must fail registration rather than produce an unstable fingerprint. The FLAC
// encoder's original `Apodizations []func([]float64)` is the case this guards:
// a closure capturing its parameter makes tukey(0.5) and tukey(0.9)
// indistinguishable even though they encode to different bitstreams.
func TestFunctionValuedFieldIsRejected(t *testing.T) {
	type window func([]float64)
	type encoderConfig struct {
		BlockSize int
		Windows   []window
	}

	schema := Struct[encoderConfig](func() encoderConfig {
		return encoderConfig{BlockSize: 4096, Windows: []window{func([]float64) {}}}
	}).
		Version("1").
		AddField(Field("blockSize", func(c *encoderConfig) *int { return &c.BlockSize }, Int())).
		AddField(Field("windows", func(c *encoderConfig) *[]window { return &c.Windows },
			Slice(NewCodec(CodecSpec[window]{
				Type:   "window",
				Decode: func(string) (window, error) { return func([]float64) {}, nil },
			})))).
		Build()

	if schema.Valid() {
		t.Fatal("a function-valued field was accepted")
	}
	found := false
	for _, item := range schema.Diagnostics() {
		if item.Path.String() == "windows" {
			found = true
		}
	}
	if !found {
		t.Fatalf("diagnostic does not point at the offending field: %v", schema.Diagnostics())
	}
}

// The data representation that replaces a function-valued field must resolve
// and fingerprint deterministically, including when the nested schema
// registers its fields in a different order.
func TestNestedSliceOfSpecsHasStableFingerprint(t *testing.T) {
	type spec struct {
		Kind      string
		Parameter float64
	}
	type encoderConfig struct{ Windows []spec }

	specSchema := func(reverse bool) Schema[spec] {
		builder := Struct[spec](func() spec { return spec{Kind: "tukey", Parameter: 0.5} }).Version("1")
		kind := Field("kind", func(s *spec) *string { return &s.Kind }, String())
		parameter := Field("parameter", func(s *spec) *float64 { return &s.Parameter }, Float64())
		if reverse {
			builder.AddField(parameter).AddField(kind)
		} else {
			builder.AddField(kind).AddField(parameter)
		}
		return builder.Build()
	}

	makeSchema := func(reverse bool) Schema[encoderConfig] {
		return Struct[encoderConfig](func() encoderConfig {
			return encoderConfig{Windows: []spec{{Kind: "tukey", Parameter: 0.5}}}
		}).Version("1").
			AddField(Field("windows", func(c *encoderConfig) *[]spec { return &c.Windows },
				Slice(Nested(specSchema(reverse))))).
			Build()
	}

	left, err := makeSchema(false).Resolve(NewPatch())
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	right, err := makeSchema(true).Resolve(NewPatch())
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if left.Fingerprint() != right.Fingerprint() {
		t.Fatalf("nested field order changed the fingerprint: %s vs %s", left.Fingerprint(), right.Fingerprint())
	}

	changed, err := makeSchema(false).ResolveValue(encoderConfig{Windows: []spec{{Kind: "tukey", Parameter: 0.9}}})
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if changed.Fingerprint() == left.Fingerprint() {
		t.Fatal("a different window parameter produced the same fingerprint")
	}
}
