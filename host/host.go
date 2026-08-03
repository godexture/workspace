// Package host is the public foundation façade for constructing an immutable
// validated catalog. Runtime resources and planning APIs are added later.
package host

import (
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/catalog"
	"github.com/godexture/godec/media/codec"
	"github.com/godexture/godec/plugin"
)

type options struct {
	plugins plugin.Set
}

// Option configures Host construction.
type Option func(*options)

// Plugins supplies the explicit immutable plugin set for this Host.
func Plugins(set plugin.Set) Option {
	return func(options *options) { options.plugins = set }
}

// Host owns one immutable catalog. It has no process-global registry, default,
// CPU feature, or resource state.
type Host struct {
	index catalog.Index
}

// New validates the supplied plugin set and returns a host only when every
// definition is valid. Errors retain all component/field diagnostics.
func New(options ...Option) (*Host, error) {
	configuration := optionsState(options)
	index, err := catalog.Build(configuration.plugins)
	if err != nil {
		return nil, err
	}
	return &Host{index: index}, nil
}

// Catalog is an immutable public view of the host's component descriptions.
type Catalog struct {
	index catalog.Index
}

// CatalogFingerprint identifies a validated host catalog composition and its
// surface-visible component/schema metadata.
type CatalogFingerprint [32]byte

// IsZero reports whether the catalog has not been built.
func (f CatalogFingerprint) IsZero() bool { return f == CatalogFingerprint{} }

// String returns the lowercase hexadecimal fingerprint.
func (f CatalogFingerprint) String() string { return hex.EncodeToString(f[:]) }

// Bytes returns a copy of the fingerprint bytes.
func (f CatalogFingerprint) Bytes() []byte {
	result := make([]byte, len(f))
	copy(result, f[:])
	return result
}

// Catalog returns the host's immutable catalog view.
func (h *Host) Catalog() Catalog {
	if h == nil {
		return Catalog{}
	}
	return Catalog{index: h.index}
}

// Components returns copied, sorted component descriptions.
func (c Catalog) Components() []plugin.ComponentView { return c.index.Views() }

// Lookup returns one component description by marker identity.
func (c Catalog) Lookup(identity plugin.Identity) (plugin.ComponentView, bool) {
	component, ok := c.index.Lookup(identity)
	if !ok {
		return plugin.ComponentView{}, false
	}
	return component.View(), true
}

// Open selects a validated component and creates its operator.
func (c Catalog) Open(identity plugin.Identity) (flow.Operator, error) {
	component, ok := c.index.Lookup(identity)
	if !ok {
		return nil, fmt.Errorf("component %q is not in the host catalog", identity)
	}
	return component.Open()
}

func (c Catalog) Bindings() []codec.Binding { return c.index.Bindings() }

// Open selects a validated component and creates its operator.
func (h *Host) Open(identity plugin.Identity) (flow.Operator, error) {
	if h == nil {
		return nil, errors.New("nil host")
	}
	return h.Catalog().Open(identity)
}

// Len reports the number of catalog components.
func (c Catalog) Len() int { return c.index.Len() }

// Fingerprint returns the stable identity of this catalog.
func (c Catalog) Fingerprint() CatalogFingerprint {
	return CatalogFingerprint(c.index.Fingerprint())
}

// Diagnostics is a helper for callers that want to inspect an aggregate host
// error without depending on catalog internals.
func Diagnostics(err error) []diagnostic.Item { return diagnostic.ItemsOf(err) }

func optionsState(values []Option) options {
	result := options{}
	for _, option := range values {
		if option != nil {
			option(&result)
		}
	}
	return result
}
