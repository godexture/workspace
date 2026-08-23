package job

import (
	"errors"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/plugin"
)

// EndpointRequest selects a typed endpoint component and its sparse config.
// Constructing it performs no scan, permission request, or I/O.
type EndpointRequest struct {
	component plugin.Identity
	config    config.Patch
}

// Adaptor explicitly selects the component and config that translate a
// direct Go resource into one typed graph boundary.
type Adaptor struct {
	component plugin.Identity
	config    config.Patch
}

func NewAdaptor(component plugin.Identity, patch config.Patch) (Adaptor, error) {
	if component.IsZero() {
		return Adaptor{}, errors.New("job direct adaptor component identity is required")
	}
	return Adaptor{component: component, config: patch.Clone()}, nil
}

func (a Adaptor) Valid() bool                { return !a.component.IsZero() }
func (a Adaptor) Component() plugin.Identity { return a.component }
func (a Adaptor) Config() config.Patch       { return a.config.Clone() }

// Direct is the type-erased Job control-plane binding. Opening remains a
// typed access.Direct[T] value and is handed only to its selected adaptor.
type Direct struct {
	adaptor   Adaptor
	opening   any
	ownership access.Ownership
	close     func() error
}

func directOf[T any](resource access.Resource[T], adaptor Adaptor) Direct {
	owned := resource
	return Direct{
		adaptor:   adaptor,
		opening:   resource.Direct(),
		ownership: resource.Ownership(),
		close:     owned.Close,
	}
}

func (d Direct) Valid() bool {
	return d.adaptor.Valid() && d.opening != nil && d.ownership.Valid() && d.close != nil
}
func (d Direct) Adaptor() Adaptor            { return d.adaptor }
func (d Direct) Opening() any                { return d.opening }
func (d Direct) Ownership() access.Ownership { return d.ownership }
func (d Direct) Close() error {
	if d.close == nil {
		return nil
	}
	return d.close()
}

func NewEndpoint(component plugin.Identity, patch config.Patch) (EndpointRequest, error) {
	if component.IsZero() {
		return EndpointRequest{}, errors.New("job endpoint component identity is required")
	}
	return EndpointRequest{component: component, config: patch.Clone()}, nil
}

func (r EndpointRequest) Valid() bool                { return !r.component.IsZero() }
func (r EndpointRequest) Component() plugin.Identity { return r.component }
func (r EndpointRequest) Config() config.Patch       { return r.config.Clone() }

type InputKind uint8

const (
	ReferenceInput InputKind = iota + 1
	SourceInput
	EndpointInput
)

func (k InputKind) Valid() bool { return k >= ReferenceInput && k <= EndpointInput }

// Input is an exclusive tagged choice. Direct source values remain behind an
// any boundary only in the job control plane; typed media items never do.
type Input struct {
	kind      InputKind
	reference access.Reference
	direct    any
	endpoint  EndpointRequest
	format    FormatSelector
	port      Port
}

func InputFromReference(reference access.Reference) (Input, error) {
	if !reference.Valid() {
		return Input{}, errors.New("job input reference is invalid")
	}
	return Input{kind: ReferenceInput, reference: reference}, nil
}

func InputFromSource[T any](source access.Resource[T], adaptor Adaptor) (Input, error) {
	if !source.Valid() || !adaptor.Valid() {
		return Input{}, errors.New("job input source ownership is invalid")
	}
	return Input{kind: SourceInput, direct: directOf(source, adaptor)}, nil
}

func InputFromEndpoint(request EndpointRequest) (Input, error) {
	if !request.Valid() {
		return Input{}, errors.New("job input endpoint is invalid")
	}
	return Input{kind: EndpointInput, endpoint: request}, nil
}

func (i Input) Valid() bool { return i.kind.Valid() && (!i.format.Kind().Valid() || i.format.Valid()) }
func (i Input) Kind() InputKind {
	return i.kind
}
func (i Input) Reference() (access.Reference, bool) {
	return i.reference, i.kind == ReferenceInput && i.reference.Valid()
}
func (i Input) Direct() (Direct, bool) {
	value, ok := i.direct.(Direct)
	return value, i.kind == SourceInput && ok && value.Valid()
}
func (i Input) Endpoint() (EndpointRequest, bool) {
	return i.endpoint, i.kind == EndpointInput && i.endpoint.Valid()
}

// WithFormatHint returns an input carrying an explicit Format hint. Content
// evidence remains authoritative when the Host resolves the hint.
func (i Input) WithFormatHint(selector FormatSelector) (Input, error) {
	if !i.Valid() || !selector.Valid() {
		return Input{}, errors.New("job input Format hint is invalid")
	}
	result := i
	result.format = selector.clone()
	return result, nil
}

func (i Input) FormatHint() (FormatSelector, bool) {
	return i.format.clone(), i.format.Valid()
}

// WithPort names the open graph port this input feeds. A job with one input
// does not need it: there is one open port and one thing to put in it. A job
// with several does, because which file arrives at which branch is a fact
// about the job rather than something to be recovered from the order the
// ports happen to sort in.
func (i Input) WithPort(value Port) (Input, error) {
	if !i.Valid() || !value.Valid() {
		return Input{}, errors.New("job input port is invalid")
	}
	result := i
	result.port = value
	return result, nil
}

func (i Input) Port() (Port, bool) { return i.port, i.port.Valid() }

type OutputKind uint8

const (
	ReferenceOutput OutputKind = iota + 1
	SinkOutput
	EndpointOutput
)

func (k OutputKind) Valid() bool { return k >= ReferenceOutput && k <= EndpointOutput }

// Output is the sink-side tagged choice. A reference does not acquire or
// truncate its target until the prepared job begins its output transaction.
type Output struct {
	kind      OutputKind
	reference access.Reference
	direct    any
	endpoint  EndpointRequest
	format    FormatSelector
	port      Port
}

func OutputToReference(reference access.Reference) (Output, error) {
	if !reference.Valid() {
		return Output{}, errors.New("job output reference is invalid")
	}
	return Output{kind: ReferenceOutput, reference: reference}, nil
}

func OutputToSink[T any](sink access.Resource[T], adaptor Adaptor) (Output, error) {
	if !sink.Valid() || !adaptor.Valid() {
		return Output{}, errors.New("job output sink ownership is invalid")
	}
	return Output{kind: SinkOutput, direct: directOf(sink, adaptor)}, nil
}

func OutputToEndpoint(request EndpointRequest) (Output, error) {
	if !request.Valid() {
		return Output{}, errors.New("job output endpoint is invalid")
	}
	return Output{kind: EndpointOutput, endpoint: request}, nil
}

func (o Output) Valid() bool { return o.kind.Valid() && (!o.format.Kind().Valid() || o.format.Valid()) }
func (o Output) Kind() OutputKind {
	return o.kind
}
func (o Output) Reference() (access.Reference, bool) {
	return o.reference, o.kind == ReferenceOutput && o.reference.Valid()
}
func (o Output) Direct() (Direct, bool) {
	value, ok := o.direct.(Direct)
	return value, o.kind == SinkOutput && ok && value.Valid()
}
func (o Output) Endpoint() (EndpointRequest, bool) {
	return o.endpoint, o.kind == EndpointOutput && o.endpoint.Valid()
}

// WithFormatRequest returns an output constrained to one requested Format.
func (o Output) WithFormatRequest(selector FormatSelector) (Output, error) {
	if !o.Valid() || !selector.Valid() {
		return Output{}, errors.New("job output Format request is invalid")
	}
	result := o
	result.format = selector.clone()
	return result, nil
}

func (o Output) FormatRequest() (FormatSelector, bool) {
	return o.format.clone(), o.format.Valid()
}

// WithPort names the open graph port this output is fed from, for the same
// reason an input names the one it feeds.
func (o Output) WithPort(value Port) (Output, error) {
	if !o.Valid() || !value.Valid() {
		return Output{}, errors.New("job output port is invalid")
	}
	result := o
	result.port = value
	return result, nil
}

func (o Output) Port() (Port, bool) { return o.port, o.port.Valid() }
