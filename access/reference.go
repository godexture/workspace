package access

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
)

var ErrInvalidReference = errors.New("access reference is invalid")

// Reference is a locator with a private canonical form and a redacted public
// display form. String formatting always uses the display form.
type Reference struct {
	canonical string
	display   string
	scheme    string
}

// ReferenceFingerprint identifies the private canonical locator without
// exposing credentials, signed parameters, or secret path material.
type ReferenceFingerprint [32]byte

func (f ReferenceFingerprint) IsZero() bool   { return f == ReferenceFingerprint{} }
func (f ReferenceFingerprint) String() string { return hex.EncodeToString(f[:]) }

// Parse creates a reference from a scheme-qualified locator. Userinfo,
// query, and fragment values remain available to the resolver through
// Canonical, but never appear in Display or String.
func Parse(value string) (Reference, error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return Reference{}, ErrInvalidReference
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || !validScheme(parsed.Scheme) {
		return Reference{}, ErrInvalidReference
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Opaque == "" && parsed.Host == "" && parsed.Path == "" {
		return Reference{}, ErrInvalidReference
	}

	display := *parsed
	display.User = nil
	if display.RawQuery != "" {
		display.RawQuery = "redacted"
	}
	if display.Fragment != "" {
		display.Fragment = "redacted"
	}
	return Reference{
		canonical: parsed.String(),
		display:   display.String(),
		scheme:    parsed.Scheme,
	}, nil
}

// NewReference constructs a scheme-qualified reference from separate parts.
func NewReference(scheme, target string) (Reference, error) {
	scheme = strings.TrimSpace(scheme)
	target = strings.TrimSpace(target)
	if !validScheme(scheme) || target == "" {
		return Reference{}, ErrInvalidReference
	}
	return Parse(strings.ToLower(scheme) + ":" + target)
}

func (r Reference) Valid() bool    { return r.canonical != "" && r.scheme != "" }
func (r Reference) Scheme() string { return r.scheme }

// Canonical returns the resolver-facing locator. Callers building logs,
// diagnostics, or user-visible plans should use Display instead.
func (r Reference) Canonical() string { return r.canonical }

// Display returns the policy-redacted locator.
func (r Reference) Display() string { return r.display }

func (r Reference) String() string { return r.Display() }

func (r Reference) Fingerprint() ReferenceFingerprint {
	if !r.Valid() {
		return ReferenceFingerprint{}
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte("godec/access/reference/v1\x00"))
	_, _ = digest.Write([]byte(r.canonical))
	var result ReferenceFingerprint
	copy(result[:], digest.Sum(nil))
	return result
}

// Format prevents %#v and other fmt forms from exposing the private
// canonical locator accidentally.
func (r Reference) Format(state fmt.State, verb rune) {
	value := r.Display()
	switch verb {
	case 'q':
		_, _ = io.WriteString(state, fmt.Sprintf("%q", value))
	case 's', 'v':
		_, _ = io.WriteString(state, value)
	default:
		_, _ = io.WriteString(state, value)
	}
}

func validScheme(value string) bool {
	if value == "" || (value[0] < 'a' || value[0] > 'z') && (value[0] < 'A' || value[0] > 'Z') {
		return false
	}
	for index := 1; index < len(value); index++ {
		char := value[index]
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '+' && char != '-' && char != '.' {
			return false
		}
	}
	return true
}
