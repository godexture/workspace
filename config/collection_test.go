package config

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/godexture/godec/diagnostic"
)

func TestMapCloneIncludesKeysAndNormalizeIncludesKeysAndValues(t *testing.T) {
	type key struct{ Value int }
	keyCodec := NewCodec(CodecSpec[*key]{
		Decode: func(value string) (*key, error) {
			var decoded int
			if _, err := fmt.Sscanf(value, "%d", &decoded); err != nil {
				return nil, err
			}
			return &key{Value: decoded}, nil
		},
		Encode: func(value *key) string { return fmt.Sprint(value.Value) },
		Canonical: func(value *key) ([]byte, error) {
			return []byte(fmt.Sprintf("key:%d", value.Value)), nil
		},
		Clone: func(value *key) *key {
			if value == nil {
				return nil
			}
			copy := *value
			return &copy
		},
	})
	codec := Map(keyCodec, Int())
	originalKey := &key{Value: 1}
	cloned := codec.Clone(map[*key]int{originalKey: 2})
	for clonedKey := range cloned {
		if clonedKey == originalKey {
			t.Fatal("map key was not cloned")
		}
		clonedKey.Value = 9
	}
	if originalKey.Value != 1 {
		t.Fatal("changing cloned key mutated original key")
	}

	normalizedKey := NewCodec(CodecSpec[int]{
		Decode: func(value string) (int, error) { return strconv.Atoi(value) },
		Encode: strconv.Itoa,
		Canonical: func(value int) ([]byte, error) {
			return []byte(fmt.Sprintf("int:%d", value)), nil
		},
		Normalize: func(value int) (int, []diagnostic.Item) { return value + 1, nil },
	})
	normalized, items := Map(normalizedKey, normalizedKey).normalizeValue(map[int]int{1: 2})
	if len(items) != 0 || normalized[2] != 3 {
		t.Fatalf("map normalize = %#v, %v", normalized, items)
	}
}
