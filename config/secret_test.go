package config

import (
	"fmt"
	"strings"
	"testing"

	"github.com/godexture/godec/diagnostic"
)

func TestSecretDoesNotLeakThroughPublicRepresentations(t *testing.T) {
	schema := testSchema()
	resolved, err := schema.Resolve(NewPatch().SetText("secret", "super-secret"))
	if err != nil {
		t.Fatalf("secret resolve failed: %v", err)
	}
	if strings.Contains(fmt.Sprint(resolved.Value()), "super-secret") || strings.Contains(resolved.String(), "super-secret") {
		t.Fatal("secret leaked through public representation")
	}
	if got := resolved.Value().Secret.Reveal(); got != "super-secret" {
		t.Fatal("secret reveal did not return the original value")
	}

	invalid := Struct[secretMarker](func() struct{ Secret SecretValue[int] } {
		return struct{ Secret SecretValue[int] }{Secret: NewSecret(0)}
	}).AddField(Field("secret", func(value *struct{ Secret SecretValue[int] }) *SecretValue[int] { return &value.Secret }, SecretCodec(Int().Range(1, 10)))).Build()
	_, err = invalid.Resolve(NewPatch())
	if err == nil || strings.Contains(err.Error(), "super-secret") || strings.Contains(err.Error(), "0") {
		t.Fatal("secret validation error leaked a raw value")
	}
}

func TestSecretSurfaceOmitsSecretAndRejectsMarker(t *testing.T) {
	type secretConfig struct {
		Endpoint string
		Token    SecretValue[string]
	}
	schema := Struct[secretConfig](func() secretConfig {
		return secretConfig{Token: NewSecret("default-secret")}
	}).
		Version("1").
		AddField(Field("endpoint", func(value *secretConfig) *string { return &value.Endpoint }, String())).
		AddField(Field("token", func(value *secretConfig) *SecretValue[string] { return &value.Token }, SecretCodec(String()))).
		Build()
	codec := Nested(schema)

	encoded := codec.Encode(secretConfig{Endpoint: "s3://bucket", Token: NewSecret("live-secret")})
	if strings.Contains(encoded, redactionMarker) || strings.Contains(encoded, "live-secret") {
		t.Fatal("secret appeared in wire encoding")
	}
	decoded, err := codec.Decode(encoded)
	if err != nil {
		t.Fatal("secret wire decode failed")
	}
	if decoded.Endpoint != "s3://bucket" || decoded.Token.Reveal() != "default-secret" {
		t.Fatal("secret wire decode did not restore the endpoint and default secret")
	}

	_, err = codec.Decode(`{"endpoint":"s3://bucket","token":"<redacted>"}`)
	if err == nil {
		t.Fatal("redaction marker was accepted by nested decode")
	}
	if strings.Contains(err.Error(), redactionMarker) || strings.Contains(err.Error(), "live-secret") {
		t.Fatal("nested decode error exposed secret data")
	}

	_, err = schema.Resolve(NewPatch().SetText("token", redactionMarker))
	if err == nil {
		t.Fatal("redaction marker was accepted by patch decode")
	}
	items := diagnostic.ItemsOf(err)
	found := false
	for _, item := range items {
		if item.Code == codeSecretRedacted {
			found = true
			if item.Message == "field input could not be decoded" || !strings.Contains(item.Message, "redaction marker") {
				t.Fatal("secret redaction message is not marker-specific")
			}
		}
		if strings.Contains(item.Message, redactionMarker) || strings.Contains(item.Message, "live-secret") {
			t.Fatal("diagnostic message exposed secret data")
		}
		for _, detail := range item.Detail {
			if strings.Contains(detail, redactionMarker) || strings.Contains(detail, "live-secret") {
				t.Fatal("diagnostic detail exposed secret data")
			}
		}
	}
	if !found {
		t.Fatal("secret redaction diagnostic missing")
	}
}

func TestResolvedSummaryOmitsSecret(t *testing.T) {
	type secretConfig struct {
		Endpoint string
		Token    SecretValue[string]
	}
	schema := Struct[secretMarker](func() secretConfig {
		return secretConfig{Token: NewSecret("default-secret")}
	}).
		Version("1").
		AddField(Field("endpoint", func(value *secretConfig) *string { return &value.Endpoint }, String())).
		AddField(Field("token", func(value *secretConfig) *SecretValue[string] { return &value.Token }, SecretCodec(String()))).
		Build()
	view := schema.View()
	resolved, err := view.Resolve(NewPatch().SetText("endpoint", "s3://bucket").SetText("token", "live-secret"))
	if err != nil {
		t.Fatal(err)
	}

	summary := resolved.Summary()
	rendered := fmt.Sprintf("%#v", summary)
	if strings.Contains(rendered, "live-secret") || strings.Contains(rendered, "default-secret") {
		t.Fatal("secret leaked through summary")
	}
	fields := summary.Fields()
	if len(fields) != 2 || fields[0].Value != "s3://bucket" || fields[1].ID != "token" || !fields[1].Redacted || fields[1].Value != "" {
		t.Fatal("summary fields did not contain the expected redacted projection")
	}

	patch, err := view.Patch(resolved)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, err := view.Resolve(patch)
	if err != nil {
		t.Fatal(err)
	}
	if roundTrip.Fingerprint() != resolved.Fingerprint() || roundTrip.Value().(secretConfig).Token.Reveal() != "live-secret" {
		t.Fatal("secret round trip failed")
	}
}

type secretMarker struct{}
