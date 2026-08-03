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
	if strings.Contains(fmt.Sprint(resolved.Value), "super-secret") || strings.Contains(resolved.String(), "super-secret") {
		t.Fatalf("secret leaked through public representation: value=%q resolved=%q", fmt.Sprint(resolved.Value), resolved.String())
	}
	if got := resolved.Value.Secret.Reveal(); got != "super-secret" {
		t.Fatalf("secret reveal = %q, want original value", got)
	}

	invalid := Struct(func() struct{ Secret SecretValue[int] } {
		return struct{ Secret SecretValue[int] }{Secret: NewSecret(0)}
	}).AddField(Field("secret", func(value *struct{ Secret SecretValue[int] }) *SecretValue[int] { return &value.Secret }, SecretCodec(Int().Range(1, 10)))).Build()
	_, err = invalid.Resolve(NewPatch())
	if err == nil || strings.Contains(err.Error(), "super-secret") || strings.Contains(err.Error(), "0") {
		t.Fatalf("secret validation error leaked a raw value: %v", err)
	}
}

func TestSecretSurfaceOmitsSecretAndRejectsMarker(t *testing.T) {
	type secretConfig struct {
		Endpoint string
		Token    SecretValue[string]
	}
	schema := Struct(func() secretConfig {
		return secretConfig{Token: NewSecret("default-secret")}
	}).
		Identity("test.secret.surface").
		Version("1").
		AddField(Field("endpoint", func(value *secretConfig) *string { return &value.Endpoint }, String())).
		AddField(Field("token", func(value *secretConfig) *SecretValue[string] { return &value.Token }, SecretCodec(String()))).
		Build()
	codec := Nested(schema)

	encoded := codec.Encode(secretConfig{Endpoint: "s3://bucket", Token: NewSecret("live-secret")})
	if strings.Contains(encoded, redactionMarker) || strings.Contains(encoded, "live-secret") {
		t.Fatalf("secret appeared in wire encoding: %q", encoded)
	}
	decoded, err := codec.Decode(encoded)
	if err != nil {
		t.Fatalf("secret wire decode failed: %v; encoded=%q", err, encoded)
	}
	if decoded.Endpoint != "s3://bucket" || decoded.Token.Reveal() != "default-secret" {
		t.Fatalf("secret wire decode = %#v, want endpoint and default secret", decoded)
	}

	_, err = codec.Decode(`{"endpoint":"s3://bucket","token":"<redacted>"}`)
	if err == nil {
		t.Fatal("redaction marker was accepted by nested decode")
	}
	if strings.Contains(err.Error(), redactionMarker) || strings.Contains(err.Error(), "live-secret") {
		t.Fatalf("nested decode error exposed secret data: %q", err)
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
				t.Fatalf("secret redaction message is not marker-specific: %#v", item)
			}
		}
		if strings.Contains(item.Message, redactionMarker) || strings.Contains(item.Message, "live-secret") {
			t.Fatalf("diagnostic message exposed secret data: %#v", item)
		}
		for _, detail := range item.Detail {
			if strings.Contains(detail, redactionMarker) || strings.Contains(detail, "live-secret") {
				t.Fatalf("diagnostic detail exposed secret data: %#v", item)
			}
		}
	}
	if !found {
		t.Fatalf("secret redaction diagnostic missing: %v", items)
	}
}
