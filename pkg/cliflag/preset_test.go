package cliflag

import "testing"

type presetConfig struct {
	Limit int
}

func (c *presetConfig) ApplyPreset(level int) {
	c.Limit = level * 10
}

func TestDecodeStructAppliesPreset(t *testing.T) {
	actual := presetConfig{}
	if err := DecodeStruct(&actual, map[string]string{"preset": "2"}); err != nil {
		t.Fatal(err)
	}
	if actual.Limit != 20 {
		t.Fatalf("DecodeStruct() = %#v", actual)
	}
}
