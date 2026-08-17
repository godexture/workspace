package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
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

func TestMapCanonicalEvaluatesEachKeyOnceAndRejectsCollisions(t *testing.T) {
	calls := make(map[int]int)
	keyCodec := NewCodec(CodecSpec[int]{
		Decode: strconv.Atoi,
		Encode: strconv.Itoa,
		Canonical: func(value int) ([]byte, error) {
			calls[value]++
			return []byte("same"), nil
		},
	})
	codec := Map(keyCodec, Int())
	value := map[int]int{1: 10, 2: 20}
	_, err := codec.Canonical(value)
	if !errors.Is(err, errMapCanonicalCollision) {
		t.Fatalf("collision error = %v, want errMapCanonicalCollision", err)
	}
	if !strings.Contains(err.Error(), "two distinct keys") {
		t.Fatalf("collision error lacks key context: %v", err)
	}
	if calls[1] != 1 || calls[2] != 1 {
		t.Fatalf("canonical calls = %#v, want one call per key", calls)
	}
	wantError := err.Error()
	for iteration := 0; iteration < 100; iteration++ {
		if _, err := codec.Canonical(value); err == nil || err.Error() != wantError {
			t.Fatalf("collision error changed across iteration %d: %v != %q", iteration, err, wantError)
		}
	}
}

func TestMapCanonicalPropagatesKeyErrorAndPanic(t *testing.T) {
	want := errors.New("key canonicalization failed")
	keyCodec := NewCodec(CodecSpec[int]{
		Decode: strconv.Atoi,
		Encode: strconv.Itoa,
		Canonical: func(value int) ([]byte, error) {
			if value == 1 {
				return nil, want
			}
			panic("secret key callback value")
		},
	})
	codec := Map(keyCodec, Int())
	_, err := codec.Canonical(map[int]int{1: 10})
	if !errors.Is(err, want) || !strings.Contains(err.Error(), "map key") {
		t.Fatalf("key error = %v, want contextual wrapped error", err)
	}
	_, err = codec.Canonical(map[int]int{2: 20})
	if err == nil || strings.Contains(err.Error(), "secret key callback value") {
		t.Fatalf("panic key error = %v", err)
	}
}

func TestMapEncodeUsesDeterministicTieBreakers(t *testing.T) {
	keyCodec := NewCodec(CodecSpec[int]{
		Decode:    strconv.Atoi,
		Encode:    strconv.Itoa,
		Canonical: func(int) ([]byte, error) { return []byte("same"), nil },
	})
	codec := Map(keyCodec, Int())
	want := codec.Encode(map[int]int{1: 10, 2: 20, 3: 30})
	for iteration := 0; iteration < 100; iteration++ {
		if got := codec.Encode(map[int]int{1: 10, 2: 20, 3: 30}); got != want {
			t.Fatalf("map Encode changed across iteration %d: %q != %q", iteration, got, want)
		}
	}
}

func TestMapCanonicalFingerprintIsStableAcrossIteration(t *testing.T) {
	keyCodec := NewCodec(CodecSpec[int]{
		Decode:    strconv.Atoi,
		Encode:    strconv.Itoa,
		Canonical: func(value int) ([]byte, error) { return []byte(fmt.Sprintf("key:%d", value)), nil },
	})
	codec := Map(keyCodec, Int())
	value := map[int]int{7: 70, 1: 10, 9: 90, 3: 30}
	canonical, err := codec.Canonical(value)
	if err != nil {
		t.Fatal(err)
	}
	want := hashCanonical(canonical)
	for iteration := 0; iteration < 100; iteration++ {
		canonical, err := codec.Canonical(value)
		if err != nil {
			t.Fatal(err)
		}
		if got := hashCanonical(canonical); got != want {
			t.Fatalf("map fingerprint changed across iteration %d", iteration)
		}
	}
}
