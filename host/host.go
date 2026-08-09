// Package host provides the public façade for immutable catalog construction,
// planning, preparation, and failure-safe job execution.
package host

import (
	"encoding/hex"
	"runtime"
	"sort"
	"time"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/internal/bind"
	"github.com/godexture/godec/internal/catalog"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

type options struct {
	plugins        plugin.Set
	platform       plan.Platform
	observation    Observation
	cleanupTimeout time.Duration
}

// Option configures Host construction.
type Option func(*options)

// Plugins supplies the explicit immutable plugin set for this Host.
func Plugins(set plugin.Set) Option {
	return func(options *options) { options.plugins = set }
}

// PlatformSnapshot overrides the immutable platform capability snapshot.
// Applications normally use the runtime-derived default; explicit injection
// supports cross-compilation hosts and deterministic conformance tests.
func PlatformSnapshot(platform plan.Platform) Option {
	return func(options *options) { options.platform = platform }
}

// Observe selects the runtime observation strategy when a prepared Job runs.
func Observe(mode Observation) Option {
	return func(options *options) { options.observation = mode }
}

// CleanupTimeout bounds cancellation joins and context-aware rollback work.
func CleanupTimeout(timeout time.Duration) Option {
	return func(options *options) { options.cleanupTimeout = timeout }
}

// Host owns one immutable catalog and platform snapshot. It has no
// process-global registry or mutable resource state.
type Host struct {
	index          catalog.Index
	platform       plan.Platform
	bindings       bind.Registry
	observation    Observation
	cleanupTimeout time.Duration
}

// New validates the supplied plugin set and returns a host only when every
// definition is valid. Errors retain all component/field diagnostics.
func New(options ...Option) (*Host, error) {
	configuration := optionsState(options)
	if !configuration.observation.Valid() || configuration.cleanupTimeout <= 0 {
		return nil, diagnostic.NewError(diagnostic.NewItem("host.runtime-policy", diagnostic.ErrorSeverity, diagnostic.Path{}, "Host runtime policy is invalid", nil))
	}
	configuration.platform.Features = append([]string(nil), configuration.platform.Features...)
	sort.Strings(configuration.platform.Features)
	if !configuration.platform.Valid() {
		return nil, diagnostic.NewError(diagnostic.NewItem("host.platform", diagnostic.ErrorSeverity, diagnostic.Path{}, "Host platform snapshot is invalid", nil))
	}
	for index, feature := range configuration.platform.Features {
		if feature == "" || index != 0 && feature == configuration.platform.Features[index-1] {
			return nil, diagnostic.NewError(diagnostic.NewItem("host.platform", diagnostic.ErrorSeverity, diagnostic.Path{}, "Host platform features are invalid", nil))
		}
	}
	index, err := catalog.Build(configuration.plugins)
	if err != nil {
		return nil, err
	}
	bindings := bind.NewRegistry(index)
	return &Host{
		index:          index,
		platform:       configuration.platform,
		bindings:       bindings,
		observation:    configuration.observation,
		cleanupTimeout: configuration.cleanupTimeout,
	}, nil
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

func (c Catalog) Declarations() []plugin.Declaration { return c.index.Declarations() }

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
	result := options{
		platform:       plan.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH, Toolchain: runtime.Version()},
		observation:    ObservationOff,
		cleanupTimeout: 5 * time.Second,
	}
	for _, option := range values {
		if option != nil {
			option(&result)
		}
	}
	return result
}
