package access

import (
	"errors"
	"fmt"
	"strings"

	"github.com/godexture/godec/plugin"
)

type providerDeclarationNamespace struct{}

// ProviderRole declares which byte-object directions a provider can resolve.
type ProviderRole uint8

const (
	SourceRole ProviderRole = iota + 1
	SinkRole
	SourceSinkRole
)

func (r ProviderRole) Valid() bool { return r >= SourceRole && r <= SourceSinkRole }

var (
	ErrInvalidProvider           = errors.New("access provider is invalid")
	ErrUnsupportedProviderScheme = errors.New("access provider scheme is not declared")
)

// Provider is a control-plane descriptor associated with a normal plugin
// component. It has no registry or runtime resolver of its own.
type Provider struct {
	identity     plugin.Identity
	schemes      []string
	role         ProviderRole
	requirements Requirements
	transaction  TransactionClass
}

type ProviderOption func(*Provider)

func WithProviderRole(role ProviderRole) ProviderOption {
	return func(provider *Provider) { provider.role = role }
}

func WithProviderRequirements(requirements Requirements) ProviderOption {
	return func(provider *Provider) { provider.requirements = cloneRequirements(requirements) }
}

func WithTransactionClass(class TransactionClass) ProviderOption {
	return func(provider *Provider) { provider.transaction = class }
}

// NewProvider associates schemes with an existing plugin component identity.
func NewProvider(identity plugin.Identity, schemes []string, options ...ProviderOption) (Provider, error) {
	if identity.IsZero() || len(schemes) == 0 {
		return Provider{}, ErrInvalidProvider
	}
	result := Provider{identity: identity, role: SourceRole}
	seen := make(map[string]struct{}, len(schemes))
	for _, value := range schemes {
		scheme := strings.ToLower(strings.TrimSpace(value))
		if !validScheme(scheme) {
			return Provider{}, fmt.Errorf("%w: %q", ErrInvalidProvider, value)
		}
		if _, exists := seen[scheme]; exists {
			return Provider{}, fmt.Errorf("%w: scheme %q is repeated", ErrInvalidProvider, scheme)
		}
		seen[scheme] = struct{}{}
		result.schemes = append(result.schemes, scheme)
	}
	for _, option := range options {
		if option != nil {
			option(&result)
		}
	}
	if !result.role.Valid() || (result.transaction != 0 && !result.transaction.Valid()) {
		return Provider{}, ErrInvalidProvider
	}
	return result, nil
}

// DefineProvider derives the target identity from a named component marker.
func DefineProvider[Marker any](schemes []string, options ...ProviderOption) (Provider, error) {
	return NewProvider(plugin.IdentityOf[Marker](), schemes, options...)
}

func (p Provider) Valid() bool {
	return !p.identity.IsZero() && len(p.schemes) > 0 && p.role.Valid() && (p.transaction == 0 || p.transaction.Valid())
}

func (p Provider) Identity() plugin.Identity          { return p.identity }
func (p Provider) Schemes() []string                  { return append([]string(nil), p.schemes...) }
func (p Provider) Role() ProviderRole                 { return p.role }
func (p Provider) Requirements() Requirements         { return cloneRequirements(p.requirements) }
func (p Provider) TransactionClass() TransactionClass { return p.transaction }

// Declaration returns the plugin declaration used by host catalog conflict
// checking. A repeated scheme across different providers is therefore a host
// construction error, with explicit plugin.Set override as the only
// replacement path.
func (p Provider) Declaration(scheme string) (plugin.Declaration, error) {
	if !p.Valid() {
		return plugin.Declaration{}, ErrInvalidProvider
	}
	scheme = strings.ToLower(strings.TrimSpace(scheme))
	if !validScheme(scheme) {
		return plugin.Declaration{}, ErrUnsupportedProviderScheme
	}
	for _, declared := range p.schemes {
		if declared == scheme {
			return plugin.Declare[providerDeclarationNamespace](scheme, p.identity), nil
		}
	}
	return plugin.Declaration{}, fmt.Errorf("%w: %s", ErrUnsupportedProviderScheme, scheme)
}

func cloneRequirements(value Requirements) Requirements {
	result := Requirements{Alternatives: make([]Alternative, len(value.Alternatives))}
	for index, alternative := range value.Alternatives {
		result.Alternatives[index] = alternative.Clone()
	}
	return result
}
