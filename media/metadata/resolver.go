package metadata

import (
	"context"
	"errors"
	"sort"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/plugin"
)

var ErrInvalidResolver = errors.New("metadata resolver is invalid")

type resolverTraitKey struct{}

var resolverKey = plugin.TraitKeyOf[resolverTraitKey]()

type resolvedEncoding struct {
	identity plugin.Identity
	value    Encoding
}

type resolverState struct {
	bindings map[carrier.ID]resolvedEncoding
	mappings []Mapping
}

// Resolver exposes only carrier Parse/Marshal operations selected by Host
// composition. It does not expose components, declarations, or the catalog.
type Resolver struct{ state *resolverState }

// NewResolver snapshots carrier-to-component resolutions and semantic mapping
// declarations from a validated composition.
func NewResolver(components map[carrier.ID]plugin.Component, mappings []Mapping) (Resolver, error) {
	bindings := make(map[carrier.ID]resolvedEncoding, len(components))
	for slot, component := range components {
		value, ok := EncodingOf(component)
		if !slot.Valid() || component.Identity().IsZero() || !ok || !value.Valid() {
			return Resolver{}, ErrInvalidResolver
		}
		bindings[slot] = resolvedEncoding{identity: component.Identity(), value: value}
	}
	if err := validateResolverMappings(mappings); err != nil {
		return Resolver{}, ErrInvalidResolver
	}
	ordered := append([]Mapping(nil), mappings...)
	sort.Slice(ordered, func(left, right int) bool { return ordered[left].Better(ordered[right]) })
	return Resolver{state: &resolverState{bindings: bindings, mappings: ordered}}, nil
}

func (r Resolver) Valid() bool { return r.state != nil }

func (r Resolver) Parse(ctx context.Context, slot carrier.ID, block BlockID, scope Scope, payload Blob) (Document, error) {
	resolved, err := r.lookup(slot)
	if err != nil {
		return Document{}, err
	}
	value, err := resolved.value.Parse(ParseContext{context: normalizeContext(ctx), carrier: slot, block: block, scope: scope, encoding: resolved.identity, payload: payload})
	if err != nil {
		return Document{}, resolverDiagnostic("metadata.parse", "metadata carrier payload could not be parsed", slot, resolved.identity, err)
	}
	return value, nil
}

func (r Resolver) Marshal(ctx context.Context, slot carrier.ID, block BlockID, document Document) (Blob, []loss.Report, error) {
	resolved, err := r.lookup(slot)
	if err != nil {
		return Blob{}, nil, err
	}
	projected, reports, err := r.project(resolved, slot, block, document)
	if err != nil {
		return Blob{}, nil, err
	}
	value, lost, err := resolved.value.Marshal(MarshalContext{context: normalizeContext(ctx), carrier: slot, block: block, encoding: resolved.identity, document: projected})
	if err != nil {
		return Blob{}, nil, resolverDiagnostic("metadata.marshal", "metadata document could not be marshalled for its carrier", slot, resolved.identity, err)
	}
	for _, value := range lost {
		reports = append(reports, loss.Report{Carrier: slot, Encoding: resolved.identity.String(), Block: string(block), Loss: value})
	}
	return value, reports, nil
}

// Project converts only entries a target encoding cannot directly represent.
// It preserves document order and multiplicity, and never chains mappings.
func (r Resolver) Project(slot carrier.ID, block BlockID, document Document) (Document, []loss.Report, error) {
	resolved, err := r.lookup(slot)
	if err != nil {
		return Document{}, nil, err
	}
	return r.project(resolved, slot, block, document)
}

func (r Resolver) project(resolved resolvedEncoding, slot carrier.ID, block BlockID, document Document) (Document, []loss.Report, error) {
	if block == "" || !document.Scope().Valid() {
		return Document{}, nil, resolverDiagnostic("metadata.project", "metadata document cannot be projected for its carrier", slot, resolved.identity, ErrInvalidContext)
	}
	builder := NewBuilder(document.Scope())
	for _, raw := range document.Blocks() {
		builder.AddBlock(raw)
	}
	var reports []loss.Report
	for _, entry := range document.entries {
		if resolved.value.Supports(entry.Key()) {
			builder.add(entry.declaration, entry.value, entry.origin)
			continue
		}
		mapped := false
		for _, mapping := range r.state.mappings {
			if mapping.Source() != entry.Key() || !resolved.value.Supports(mapping.Target()) {
				continue
			}
			converted, ok := mapping.Convert(entry.value)
			if !ok {
				continue
			}
			builder.add(mapping.targetDeclaration, converted, Origin{})
			reports = append(reports, loss.Report{
				Carrier: slot, Encoding: resolved.identity.String(), Block: string(block),
				Loss: loss.Loss{Key: entry.Key(), Kind: loss.Converted, Target: mapping.Target(), Mapping: mapping.Lossiness(), Detail: "metadata.mapping", Source: entry.origin.LossOrigin()},
			})
			mapped = true
			break
		}
		if !mapped {
			builder.add(entry.declaration, entry.value, entry.origin)
		}
	}
	projected, err := builder.Build()
	if err != nil {
		return Document{}, nil, resolverDiagnostic("metadata.project", "metadata document cannot be projected for its carrier", slot, resolved.identity, err)
	}
	return projected, reports, nil
}

func validateResolverMappings(values []Mapping) error {
	seen := make(map[mappingContract]struct{}, len(values))
	for _, value := range values {
		if !value.Valid() {
			return ErrInvalidResolver
		}
		contract := value.contract()
		if _, exists := seen[contract]; exists {
			return ErrInvalidResolver
		}
		seen[contract] = struct{}{}
	}
	return nil
}

func (r Resolver) lookup(slot carrier.ID) (resolvedEncoding, error) {
	if !r.Valid() || !slot.Valid() {
		return resolvedEncoding{}, ErrInvalidResolver
	}
	value, ok := r.state.bindings[slot]
	if !ok {
		return resolvedEncoding{}, resolverDiagnostic("metadata.binding-missing", "metadata carrier has no encoding binding", slot, plugin.Identity{}, nil)
	}
	return value, nil
}

// WithResolver attaches one immutable resolver to a node-local CompileContext.
func WithResolver(ctx plugin.CompileContext, resolver Resolver) (plugin.CompileContext, error) {
	if !resolver.Valid() {
		return ctx, ErrInvalidResolver
	}
	return plugin.CompileContextWithTrait(ctx, resolverKey, resolver)
}

// ResolverOf returns the narrow resolver prepared for one Format node.
func ResolverOf(ctx plugin.CompileContext) (Resolver, bool) {
	value, ok := plugin.TraitValueOf[Resolver](ctx, resolverKey)
	return value, ok && value.Valid()
}

func resolverDiagnostic(code, message string, slot carrier.ID, encoding plugin.Identity, cause error) error {
	detail := map[string]string{
		"carrier": slot.String(),
		"binding": BindingKey(slot).String(),
	}
	path := diagnostic.Path{Descriptor: BindingKey(slot).String()}
	if !encoding.IsZero() {
		detail["encoding"] = encoding.String()
		path.Component = encoding.String()
	}
	if cause != nil {
		detail["cause"] = cause.Error()
	}
	structured := diagnostic.NewError(diagnostic.NewItem(code, diagnostic.ErrorSeverity, path, message, detail))
	if cause != nil {
		return errors.Join(structured, cause)
	}
	return structured
}

func normalizeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
