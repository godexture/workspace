package engine

import (
	"strings"
	"testing"
)

type testResolvedConfig struct {
	value int
}

func (c testResolvedConfig) Validate() error {
	return nil
}

type testConfigWrapper struct {
	value int
}

func (c testConfigWrapper) Resolve() testResolvedConfig {
	return testResolvedConfig{value: c.value}
}

func (testConfigWrapper) ResolveDefault() testResolvedConfig {
	return testResolvedConfig{value: 42}
}

type wrongConfigWrapper struct{}

func TestResolveConfigUsesDefaultForNil(t *testing.T) {
	t.Parallel()
	got, err := ResolveConfig[testResolvedConfig, testConfigWrapper](nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.value != 42 {
		t.Fatalf("value = %d, want 42", got.value)
	}
}

func TestResolveConfigAcceptsValueAndPointer(t *testing.T) {
	t.Parallel()
	for name, config := range map[string]any{
		"value":   testConfigWrapper{value: 1},
		"pointer": &testConfigWrapper{value: 2},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := ResolveConfig[testResolvedConfig, testConfigWrapper](config)
			if err != nil {
				t.Fatal(err)
			}
			if got.value == 0 {
				t.Fatal("configuration was not resolved")
			}
		})
	}
}

func TestResolveConfigRejectsWrongType(t *testing.T) {
	t.Parallel()
	_, err := ResolveConfig[testResolvedConfig, testConfigWrapper](wrongConfigWrapper{})
	if err == nil {
		t.Fatal("wrong configuration type was accepted")
	}
	if !strings.Contains(err.Error(), "unexpected configuration type") {
		t.Fatalf("unexpected error: %v", err)
	}
}
