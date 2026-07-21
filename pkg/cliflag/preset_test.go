package cliflag

import "testing"

type presetConfig struct {
	Limit int `name:"limit"`
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

func TestDescribeStructIncludesPreset(t *testing.T) {
	descriptions, err := DescribeStruct(&presetConfig{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(descriptions) != 2 || descriptions[0].Name != "preset" || descriptions[0].Default != "-1" {
		t.Fatalf("DescribeStruct() = %#v", descriptions)
	}
}
