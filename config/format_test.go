package config

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

var protectedFormats = []string{
	"%v",
	"%+v",
	"%#v",
	"%b",
	"%c",
	"%d",
	"%o",
	"%O",
	"%x",
	"%X",
	"%U",
	"%e",
	"%E",
	"%f",
	"%F",
	"%g",
	"%G",
	"%s",
	"%q",
	"%+q",
	"%#q",
	"%p",
	"%T",
	"%w",
	"%z",
	"%20v",
	"%.5v",
	"%+20.5v",
}

type revealingValue string

func (v revealingValue) String() string { return string(v) }

func (v revealingValue) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte(v))
}

func TestSecretValueFormattingIsRedactedInContainers(t *testing.T) {
	const raw = "r01-secret-value"
	secret := NewSecret(revealingValue(raw))

	type publicContainer struct {
		Secret SecretValue[revealingValue]
	}
	type privateContainer struct {
		secret SecretValue[revealingValue]
	}
	type secretAlias SecretValue[revealingValue]

	values := []formatValue{
		{name: "value", value: secret},
		{name: "pointer", value: &secret},
		{name: "public struct", value: publicContainer{Secret: secret}},
		{name: "public struct pointer", value: &publicContainer{Secret: secret}},
		{name: "private struct", value: privateContainer{secret: secret}},
		{name: "private struct pointer", value: &privateContainer{secret: secret}},
		{name: "slice", value: []SecretValue[revealingValue]{secret}},
		{name: "map", value: map[string]SecretValue[revealingValue]{"token": secret}},
		{name: "interface", value: any(secret)},
		{name: "alias", value: secretAlias(secret)},
		{name: "reflect value", value: reflect.ValueOf(secret)},
	}
	assertProtectedFormatting(t, raw, values)

	if fmt.Sprint(secret) != redactionMarker || fmt.Sprintf("%q", secret) != fmt.Sprintf("%q", redactionMarker) {
		t.Fatal("secret formatter did not emit the redaction marker")
	}
	if secret.Reveal() != revealingValue(raw) {
		t.Fatal("secret reveal did not return the original value")
	}
	var zero SecretValue[string]
	if zero.Reveal() != "" {
		t.Fatal("zero secret did not reveal the element zero value")
	}
}

func TestPatchFormattingHidesAllValuesInContainers(t *testing.T) {
	const raw = "r01-patch-value"
	patch := NewPatch().
		Preset("fast").
		SetText("token", raw).
		Set("typed", map[string]any{"nested": revealingValue(raw)}).
		Planned()

	type publicContainer struct {
		Patch Patch
	}
	type privateContainer struct {
		patch Patch
	}
	type patchAlias Patch

	values := []formatValue{
		{name: "value", value: patch},
		{name: "pointer", value: &patch},
		{name: "public struct", value: publicContainer{Patch: patch}},
		{name: "public struct pointer", value: &publicContainer{Patch: patch}},
		{name: "private struct", value: privateContainer{patch: patch}},
		{name: "private struct pointer", value: &privateContainer{patch: patch}},
		{name: "slice", value: []Patch{patch}},
		{name: "map", value: map[string]Patch{"config": patch}},
		{name: "interface", value: any(patch)},
		{name: "alias", value: patchAlias(patch)},
	}
	assertProtectedFormatting(t, raw, values)

	const want = `config patch preset="fast" fields=["token":planner "typed":planner]`
	if patch.String() != want {
		t.Fatal("patch formatter did not preserve safe patch metadata")
	}
}

func TestResolvedFormattingIsRedactedInContainers(t *testing.T) {
	const raw = "r01-resolved-value"
	type secretConfig struct {
		Token SecretValue[string]
	}
	schema := Struct[secretConfig](func() secretConfig {
		return secretConfig{Token: NewSecret("default")}
	}).
		Version("1").
		AddField(Field("token", func(value *secretConfig) *SecretValue[string] { return &value.Token }, SecretCodec(String()))).
		Build()
	resolved, err := schema.Resolve(NewPatch().SetText("token", raw))
	if err != nil {
		t.Fatal("typed secret config did not resolve")
	}
	view, err := schema.View().Resolve(NewPatch().SetText("token", raw))
	if err != nil {
		t.Fatal("type-erased secret config did not resolve")
	}

	type publicContainer struct {
		Resolved Resolved[secretConfig]
		View     ResolvedView
	}
	type privateContainer struct {
		resolved Resolved[secretConfig]
		view     ResolvedView
	}

	values := []formatValue{
		{name: "resolved", value: resolved},
		{name: "resolved pointer", value: &resolved},
		{name: "view", value: view},
		{name: "view pointer", value: &view},
		{name: "resolved value", value: resolved.Value},
		{name: "public struct", value: publicContainer{Resolved: resolved, View: view}},
		{name: "public struct pointer", value: &publicContainer{Resolved: resolved, View: view}},
		{name: "private struct", value: privateContainer{resolved: resolved, view: view}},
		{name: "private struct pointer", value: &privateContainer{resolved: resolved, view: view}},
		{name: "resolved slice", value: []Resolved[secretConfig]{resolved}},
		{name: "view slice", value: []ResolvedView{view}},
		{name: "resolved map", value: map[string]Resolved[secretConfig]{"config": resolved}},
		{name: "view map", value: map[string]ResolvedView{"config": view}},
		{name: "resolved interface", value: any(resolved)},
		{name: "view interface", value: any(view)},
	}
	assertProtectedFormatting(t, raw, values)
}

type formatValue struct {
	name  string
	value any
}

func assertProtectedFormatting(t *testing.T, raw string, values []formatValue) {
	t.Helper()
	for _, value := range values {
		for _, format := range protectedFormats {
			if strings.Contains(fmt.Sprintf(format, value.value), raw) {
				t.Errorf("%s formatting with %s exposed protected data", value.name, format)
			}
		}
	}
}
