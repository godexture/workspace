package config

import "testing"

func TestIntegerDecodeChecksTargetWidth(t *testing.T) {
	if _, err := parseInt[int8]("128"); err == nil {
		t.Fatal("int8 overflow was accepted")
	}
	if _, err := parseUint[uint8]("256"); err == nil {
		t.Fatal("uint8 overflow was accepted")
	}
}
