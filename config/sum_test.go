package config

import (
	"fmt"
	"testing"
)

func TestStandardSumTypesAndCustomCodec(t *testing.T) {
	optional := OptionalCodec(Int())
	if got, err := optional.Decode("none"); err != nil || got.Present {
		t.Fatalf("optional none = %#v, %v", got, err)
	}
	auto := AutoCodec(Int())
	if got, err := auto.Decode("auto"); err != nil || got.Mode != AutoModeAuto {
		t.Fatalf("auto = %#v, %v", got, err)
	}
	if got, err := RateCodec().Decode("48000"); err != nil || got != FixedRate(48000) {
		t.Fatalf("rate = %#v, %v", got, err)
	}

	type custom int
	customCodec := NewCodec(CodecSpec[custom]{
		Decode: func(value string) (custom, error) {
			parsed, err := Int().Decode(value)
			return custom(parsed), err
		},
		Encode: func(value custom) string { return fmt.Sprintf("custom:%d", value) },
		Canonical: func(value custom) ([]byte, error) {
			return []byte(fmt.Sprintf("custom:%d", value)), nil
		},
	})
	if !customCodec.Valid() {
		t.Fatal("custom codec is invalid")
	}
}
