package access

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/godexture/godec/plugin"
)

type sourceTraitKey struct{}
type sinkTraitKey struct{}

var (
	sourceKey = plugin.TraitKeyOf[sourceTraitKey]()
	sinkKey   = plugin.TraitKeyOf[sinkTraitKey]()

	ErrInvalidTrait = errors.New("access trait is invalid")
)

// Session is one acquired byte-object session. Its reported capabilities are
// revalidated by Prepare before the selected narrow view is handed to a
// component.
type Session interface {
	Capabilities() Capabilities
	Close() error
}

// AcquireFunc opens one job-local session for a canonical reference and the
// capability alternative selected by planning.
type AcquireFunc func(context.Context, Reference, Selection) (Session, error)

// SourceTrait is the typed access view of a source component trait.
type SourceTrait struct {
	scheme       string
	capabilities Capabilities
	acquire      AcquireFunc
}

// Source attaches an Access source trait to a 0-input/1-output component.
func Source(scheme string, capabilities Capabilities, acquire AcquireFunc) plugin.ComponentOption {
	trait := SourceTrait{
		scheme:       normalizeScheme(scheme),
		capabilities: cloneCapabilities(capabilities),
		acquire:      acquire,
	}
	return plugin.WithTrait(sourceKey, trait.manifest(), trait)
}

// SourceOf returns the typed source trait attached to component.
func SourceOf(component plugin.Component) (SourceTrait, bool) {
	trait, ok := plugin.TraitValueOf[SourceTrait](component, sourceKey)
	return trait, ok
}

func (t SourceTrait) Valid() bool {
	return validScheme(t.scheme) && len(t.capabilities.Values()) != 0 && t.capabilities.Valid() && t.acquire != nil
}

func (t SourceTrait) Scheme() string             { return t.scheme }
func (t SourceTrait) Capabilities() Capabilities { return cloneCapabilities(t.capabilities) }

func (t SourceTrait) Acquire(ctx context.Context, reference Reference, selected Selection) (Session, error) {
	if !t.Valid() {
		return nil, ErrInvalidTrait
	}
	return t.acquire(ctx, reference, selected)
}

func (t SourceTrait) manifest() string {
	return traitManifest("source", t.scheme, t.capabilities, 0)
}

// SinkTrait is the typed access view of a sink component trait.
type SinkTrait struct {
	scheme       string
	capabilities Capabilities
	transaction  TransactionClass
	acquire      AcquireFunc
}

// Sink attaches an Access sink trait to a 1-input/0-output component.
func Sink(scheme string, capabilities Capabilities, transaction TransactionClass, acquire AcquireFunc) plugin.ComponentOption {
	trait := SinkTrait{
		scheme:       normalizeScheme(scheme),
		capabilities: cloneCapabilities(capabilities),
		transaction:  transaction,
		acquire:      acquire,
	}
	return plugin.WithTrait(sinkKey, trait.manifest(), trait)
}

// SinkOf returns the typed sink trait attached to component.
func SinkOf(component plugin.Component) (SinkTrait, bool) {
	trait, ok := plugin.TraitValueOf[SinkTrait](component, sinkKey)
	return trait, ok
}

func (t SinkTrait) Valid() bool {
	return validScheme(t.scheme) && len(t.capabilities.Values()) != 0 && t.capabilities.Valid() && t.transaction.Valid() && t.acquire != nil
}

func (t SinkTrait) Scheme() string                     { return t.scheme }
func (t SinkTrait) Capabilities() Capabilities         { return cloneCapabilities(t.capabilities) }
func (t SinkTrait) TransactionClass() TransactionClass { return t.transaction }

func (t SinkTrait) Acquire(ctx context.Context, reference Reference, selected Selection) (Session, error) {
	if !t.Valid() {
		return nil, ErrInvalidTrait
	}
	return t.acquire(ctx, reference, selected)
}

func (t SinkTrait) manifest() string {
	return traitManifest("sink", t.scheme, t.capabilities, t.transaction)
}

func normalizeScheme(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func cloneCapabilities(value Capabilities) Capabilities {
	result, _ := NewCapabilities(value.Values()...)
	return result
}

func traitManifest(direction, scheme string, capabilities Capabilities, transaction TransactionClass) string {
	values := capabilities.Values()
	names := make([]string, len(values))
	for index, capability := range values {
		names[index] = string(capability)
	}
	return direction + "|scheme=" + scheme + "|cap=" + strings.Join(names, ",") + "|transaction=" + strconv.Itoa(int(transaction))
}
