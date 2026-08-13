package config

import (
	"strings"
	"testing"

	"github.com/godexture/godec/diagnostic"
)

const (
	codeTrimmed        = "test.trimmed"
	compositionSecret  = "composition-secret"
	compositionPadding = "  "
)

type compositionNested struct {
	Name string
}

type compositionConfig struct {
	Items  []Optional[string]
	Labels map[string]Auto[string]
	Nested compositionNested
	Choice Union[string]
	Token  SecretValue[string]
}

// trimmedString normalizes by trimming and reports what it trimmed. The raw
// value is deliberately part of the message and detail so a composition that
// forwards diagnostics out of a secret is visible as a leak.
func trimmedString() Codec[string] {
	return NewCodec(CodecSpec[string]{
		Type:      "trimmed",
		Decode:    func(value string) (string, error) { return value, nil },
		Encode:    func(value string) string { return value },
		Canonical: func(value string) ([]byte, error) { return []byte("trimmed:" + value), nil },
		Clone:     func(value string) string { return value },
		Normalize: func(value string) (string, []diagnostic.Item) {
			trimmed := strings.TrimSpace(value)
			if trimmed == value {
				return value, nil
			}
			return trimmed, []diagnostic.Item{diagnostic.NewItem(codeTrimmed, diagnostic.WarningSeverity, diagnostic.Path{}, "trimmed "+value, map[string]string{"raw": value})}
		},
	})
}

func compositionSchema() Schema[compositionConfig] {
	nested := Struct[compositionNested](func() compositionNested {
		return compositionNested{Name: compositionPadding + "nested" + compositionPadding}
	}).
		Version("1").
		AddField(Field("name", func(value *compositionNested) *string { return &value.Name }, trimmedString())).
		Build()

	return Struct[compositionConfig](func() compositionConfig {
		return compositionConfig{
			Items:  []Optional[string]{Some(compositionPadding + "item" + compositionPadding)},
			Labels: map[string]Auto[string]{"key": ValueOf(compositionPadding + "label" + compositionPadding)},
			Nested: compositionNested{Name: compositionPadding + "nested" + compositionPadding},
			Choice: Union[string]{Variant: "text", Value: compositionPadding + "choice" + compositionPadding},
			Token:  NewSecret(compositionPadding + compositionSecret + compositionPadding),
		}
	}).
		Version("1").
		AddField(Field("items", func(value *compositionConfig) *[]Optional[string] { return &value.Items }, Slice(OptionalCodec(trimmedString())))).
		AddField(Field("labels", func(value *compositionConfig) *map[string]Auto[string] { return &value.Labels }, Map(String(), AutoCodec(trimmedString())))).
		AddField(Field("nested", func(value *compositionConfig) *compositionNested { return &value.Nested }, Nested(nested))).
		AddField(Field("choice", func(value *compositionConfig) *Union[string] { return &value.Choice }, UnionCodec(UnionChoice[string]{ID: "text", Codec: trimmedString()}))).
		AddField(Field("token", func(value *compositionConfig) *SecretValue[string] { return &value.Token }, SecretCodec(trimmedString()))).
		Build()
}

// A codec that normalizes on its own must normalize the same way through
// every combinator. Otherwise the value, its provenance, its diagnostics, and
// its fingerprint all depend on how the schema was composed.
func TestCompositionKeepsNormalization(t *testing.T) {
	schema := compositionSchema()
	if err := schema.Err(); err != nil {
		t.Fatalf("composition schema is invalid: %v", err)
	}
	resolved, err := schema.Resolve(NewPatch())
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}

	if got := resolved.Value().Items[0].Value; got != "item" {
		t.Errorf("slice element = %q, want %q", got, "item")
	}
	if got := resolved.Value().Labels["key"].Value; got != "label" {
		t.Errorf("map value = %q, want %q", got, "label")
	}
	if got := resolved.Value().Nested.Name; got != "nested" {
		t.Errorf("nested field = %q, want %q", got, "nested")
	}
	if got := resolved.Value().Choice.Value; got != "choice" {
		t.Errorf("union variant value = %q, want %q", got, "choice")
	}
	if got := resolved.Value().Token.Reveal(); got != compositionSecret {
		t.Errorf("secret value = %q, want %q", got, compositionSecret)
	}

	for _, field := range []string{"items", "labels", "nested", "choice", "token"} {
		source, ok := resolved.Provenance().Source(field)
		if !ok || source != SourceNormalized {
			t.Errorf("provenance for %s = %v (present %t), want normalized", field, source, ok)
		}
	}
}

// Diagnostics raised inside a combinator must reach the caller at the path
// that identifies the offending element.
func TestCompositionReportsNormalizationPaths(t *testing.T) {
	resolved, err := compositionSchema().Resolve(NewPatch())
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	paths := make(map[string]diagnostic.Severity)
	for _, item := range resolved.Diagnostics() {
		if item.Code == codeTrimmed {
			paths[item.Path.String()] = item.Severity
		}
	}
	for _, path := range []string{"items.0", "labels.key", "nested.name", "choice.value"} {
		severity, ok := paths[path]
		if !ok {
			t.Errorf("no normalization diagnostic at %s; got %v", path, paths)
			continue
		}
		if severity != diagnostic.WarningSeverity {
			t.Errorf("diagnostic at %s has severity %v, want warning", path, severity)
		}
	}
}

// A secret keeps the severity and code of an inner diagnostic and nothing
// else: the message, the detail map, and the value-derived path all describe
// the secret itself.
func TestCompositionRedactsSecretDiagnostics(t *testing.T) {
	resolved, err := compositionSchema().Resolve(NewPatch())
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	found := false
	for _, item := range resolved.Diagnostics() {
		if !strings.HasPrefix(item.Path.String(), "token") {
			continue
		}
		found = true
		if item.Code != codeTrimmed || item.Severity != diagnostic.WarningSeverity {
			t.Errorf("secret diagnostic = %s/%v, want %s/warning", item.Code, item.Severity, codeTrimmed)
		}
		if len(item.Detail) != 0 {
			t.Errorf("secret diagnostic kept detail %v", item.Detail)
		}
	}
	if !found {
		t.Errorf("secret normalization produced no diagnostic; got %v", resolved.Diagnostics())
	}
	for _, item := range resolved.Diagnostics() {
		if strings.Contains(item.Message, compositionSecret) {
			t.Errorf("secret leaked into message %q", item.Message)
		}
		for key, value := range item.Detail {
			if strings.Contains(key+value, compositionSecret) {
				t.Errorf("secret leaked into detail %s=%s", key, value)
			}
		}
	}
}

// Fingerprints identify normalized values, so a value supplied already
// normalized must fingerprint the same as one normalized during resolution.
func TestCompositionFingerprintsNormalizedValue(t *testing.T) {
	schema := compositionSchema()
	resolved, err := schema.Resolve(NewPatch())
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	direct, err := schema.ResolveValue(compositionConfig{
		Items:  []Optional[string]{Some("item")},
		Labels: map[string]Auto[string]{"key": ValueOf("label")},
		Nested: compositionNested{Name: "nested"},
		Choice: Union[string]{Variant: "text", Value: "choice"},
		Token:  NewSecret(compositionSecret),
	})
	if err != nil {
		t.Fatalf("resolve of the normalized value failed: %v", err)
	}
	if resolved.Fingerprint() != direct.Fingerprint() {
		t.Errorf("normalized fingerprint %s differs from the pre-normalized one %s", resolved.Fingerprint(), direct.Fingerprint())
	}
}
