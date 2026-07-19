package main

import (
	"slices"
	"testing"
)

func TestEnableExperiment(t *testing.T) {
	tests := map[string]string{
		"":                  "simd",
		"simd":              "simd",
		"nosimd":            "simd",
		"other":             "other,simd",
		"other,simd":        "other,simd",
		"other,nosimd,more": "other,more,simd",
	}
	for input, expected := range tests {
		if actual := enableExperiment(input, "simd"); actual != expected {
			t.Errorf("enableExperiment(%q, %q) = %q, want %q", input, "simd", actual, expected)
		}
	}
}

func TestSetEnv(t *testing.T) {
	env := setEnv([]string{"A=1", "GOEXPERIMENT=nosimd", "B=2"}, "GOEXPERIMENT", "simd")
	expected := []string{"A=1", "B=2", "GOEXPERIMENT=simd"}
	if !slices.Equal(env, expected) {
		t.Fatalf("setEnv() = %q, want %q", env, expected)
	}
}
