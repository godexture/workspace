package config

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/godexture/godec/diagnostic"
)

// SecretValue stores a value whose display and canonical form are redacted.
// Reveal is an explicit capability used only by the component that needs the
// secret for execution.
type SecretValue[T any] struct {
	// A closure keeps the raw value opaque even when a named type bypasses
	// SecretValue's formatting methods.
	reveal func() T
}

// NewSecret constructs an opaque secret value.
func NewSecret[T any](value T) SecretValue[T] {
	return SecretValue[T]{reveal: func() T { return value }}
}

// Reveal returns the secret to the caller that explicitly requested it.
func (value SecretValue[T]) Reveal() T {
	if value.reveal == nil {
		var zero T
		return zero
	}
	return value.reveal()
}

func (value SecretValue[T]) String() string { return redactionMarker }

// Format prevents every fmt verb, including %#v, from traversing the stored
// value. Reveal is the only operation that exposes it.
func (value SecretValue[T]) Format(state fmt.State, verb rune) {
	writeSafeFormat(state, verb, redactionMarker)
}

// SecretCodec wraps a codec and ensures raw values never enter human or
// canonical output. Its Encode result is display-only; Schema wire encoding
// omits secret fields. The fingerprint contains a domain-separated digest so
// equal secrets remain distinguishable without exposing the secret.
func SecretCodec[T any](inner Codec[T]) Codec[SecretValue[T]] {
	result := NewCodec(CodecSpec[SecretValue[T]]{
		Type: "secret<" + inner.description.Type + ">",
		Decode: func(value string) (SecretValue[T], error) {
			if value == redactionMarker {
				return SecretValue[T]{}, decodeError{code: codeSecretRedacted, message: "secret input is not accepted"}
			}
			decoded, err := inner.Decode(value)
			if err != nil {
				return SecretValue[T]{}, fmt.Errorf("secret value is invalid")
			}
			return NewSecret(decoded), nil
		},
		Encode: func(SecretValue[T]) string { return redactionMarker },
		Canonical: func(value SecretValue[T]) ([]byte, error) {
			canonical, err := inner.Canonical(value.Reveal())
			if err != nil {
				return nil, fmt.Errorf("secret value cannot be canonicalized")
			}
			hash := sha256.New()
			hash.Write([]byte("godec/config/secret/v1\x00"))
			writeLength(hash, canonical)
			return hash.Sum(nil), nil
		},
		Clone: func(value SecretValue[T]) SecretValue[T] {
			return NewSecret(inner.Clone(value.Reveal()))
		},
		Normalize: func(value SecretValue[T]) (SecretValue[T], []diagnostic.Item) {
			normalized, items := inner.normalizeValue(value.Reveal())
			return NewSecret(normalized), redactItems(items)
		},
		Validate: func(value SecretValue[T]) []diagnostic.Item {
			return redactItems(inner.validateValue(value.Reveal()))
		},
		Description: Description{Type: "secret<" + inner.description.Type + ">", Secret: true},
	})
	if !inner.Valid() {
		result = result.addConstruction(diagnostic.NewItem("config.invalid-secret-codec", diagnostic.ErrorSeverity, diagnostic.Path{}, "secret inner codec must be valid", nil))
	}
	return result
}

// redactItems carries an inner diagnostic out of a secret without carrying
// what the inner codec was looking at. Severity and the stable code survive so
// an error stays an error and a warning stays a warning; the message, the
// detail map, and the value-derived field path do not, because a map key or a
// slice element description inside a secret is itself secret.
func redactItems(items []diagnostic.Item) []diagnostic.Item {
	if len(items) == 0 {
		return nil
	}
	result := make([]diagnostic.Item, 0, len(items))
	for _, item := range items {
		code := item.Code
		if code == "" {
			code = codeSecretInvalid
		}
		path := diagnostic.Path{Component: item.Path.Component, Descriptor: item.Path.Descriptor}
		result = append(result, diagnostic.NewItem(code, item.Severity, path, "secret value reported a redacted "+item.Severity.String(), nil))
	}
	return result
}

func writeLength(destination interface{ Write([]byte) (int, error) }, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = destination.Write(length[:])
	_, _ = destination.Write(value)
}
