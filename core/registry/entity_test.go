package registry

import "testing"

type nestedMutableConfig struct {
	Tags   []string
	Extra  map[string]int
	Nested *nestedMutableConfig
}

func TestStaticConfigurationFactoryClonesNestedMutableFields(t *testing.T) {
	t.Parallel()
	original := &nestedMutableConfig{
		Tags:   []string{"a", "b"},
		Extra:  map[string]int{"x": 1},
		Nested: &nestedMutableConfig{Tags: []string{"c"}},
	}
	factory := StaticConfigurationFactory(original)

	first, err := factory.New()
	if err != nil {
		t.Fatal(err)
	}
	second := factory.Default()

	firstConfig := first.(*nestedMutableConfig)
	secondConfig := second.(*nestedMutableConfig)

	firstConfig.Tags[0] = "mutated"
	firstConfig.Extra["x"] = 99
	firstConfig.Nested.Tags[0] = "mutated"

	if secondConfig.Tags[0] == "mutated" {
		t.Fatal("mutating one clone's slice mutated another clone")
	}
	if secondConfig.Extra["x"] == 99 {
		t.Fatal("mutating one clone's map mutated another clone")
	}
	if secondConfig.Nested.Tags[0] == "mutated" {
		t.Fatal("mutating one clone's nested pointer mutated another clone")
	}
	if original.Tags[0] == "mutated" {
		t.Fatal("mutating a clone's slice mutated the original")
	}
}
