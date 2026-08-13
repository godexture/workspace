package acme

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"sync"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
)

const (
	scheme         = "acme"
	maxLabelBytes  = 32
	maxObjectBytes = 256
)

var ErrMalformed = errors.New("malformed ACME stream")

var sourceCaps = mustCapabilities(access.SequentialRead, access.RandomRead, access.StableSize)

type sourcePlan struct{ shape flow.Shape }

func Encode(label string, payload []byte) ([]byte, error) {
	if label == "" || len(label) > maxLabelBytes || len(payload) == 0 || 5+len(label)+len(payload) > maxObjectBytes {
		return nil, ErrMalformed
	}
	result := make([]byte, 5+len(label)+len(payload))
	copy(result[0:4], "ACM1")
	result[4] = byte(len(label))
	copy(result[5:], label)
	copy(result[5+len(label):], payload)
	return result, nil
}

func Reference(encoded []byte) (access.Reference, error) {
	if len(encoded) == 0 || len(encoded) > maxObjectBytes {
		return access.Reference{}, ErrMalformed
	}
	return access.NewReference(scheme, hex.EncodeToString(encoded))
}

func sourceComponent() plugin.Component {
	shape := flow.NewShape(nil, []flow.Port{flow.Out("bytes", access.Bytes())})
	spec := plugin.Spec[configuration, sourcePlan, stream.Descriptor]{
		Shape: plugin.StaticShape[configuration](shape),
		Compile: func(plugin.CompileContext, configuration, flow.Descriptors[stream.Descriptor]) (plugin.Compiled[sourcePlan, stream.Descriptor], error) {
			descriptor, err := stream.NewDescriptor("acme", access.Bytes().Identity(), access.CarrierTimeBase(), property.New())
			if err != nil {
				return plugin.Compiled[sourcePlan, stream.Descriptor]{}, err
			}
			return plugin.Compiled[sourcePlan, stream.Descriptor]{
				Plan: sourcePlan{shape: shape.Clone()}, Outputs: flow.NewDescriptors(flow.Describe("bytes", descriptor)),
				Resources: resource.Request{Memory: maxObjectBytes},
			}, nil
		},
		Open: func(ctx plugin.OpenContext, plan sourcePlan) (flow.Operator, error) {
			opening, ok := plugin.Boundary[access.Opening](ctx)
			if !ok || ctx.Buffers() == nil {
				return nil, errors.New("ACME source requires an Access opening and payload grant")
			}
			sequential, _ := access.SequentialOf(opening)
			random, _ := access.RandomOf(opening)
			if sequential == nil && random == nil {
				return nil, errors.New("ACME source has no selected read view")
			}
			return &sourceOperator{shape: plan.shape.Clone(), buffers: ctx.Buffers(), sequential: sequential, random: random}, nil
		},
	}
	return plugin.NewComponent[sourceID](plugin.Descriptor{DisplayName: "ACME object source"}, configurationSchema(),
		plugin.WithSpec(spec), plugin.WithReader("bytes", access.Bytes()), access.Source(scheme, sourceCaps, acquireSource))
}

type memorySession struct {
	mu     sync.Mutex
	data   []byte
	offset int
	closed bool
}

func acquireSource(ctx context.Context, reference access.Reference, selected access.Selection) (access.Session, error) {
	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	if reference.Scheme() != scheme || !selected.ValidFor(access.SourceDirection) {
		return nil, access.ErrInvalidCapabilities
	}
	prefix := scheme + ":"
	canonical := reference.Canonical()
	if !strings.HasPrefix(canonical, prefix) {
		return nil, access.ErrInvalidReference
	}
	data, err := hex.DecodeString(strings.TrimPrefix(canonical, prefix))
	if err != nil || len(data) == 0 || len(data) > maxObjectBytes {
		return nil, ErrMalformed
	}
	return &memorySession{data: data}, nil
}

func (*memorySession) Capabilities() access.Capabilities { return sourceCaps }

func (s *memorySession) Read(ctx context.Context, destination []byte) (int, error) {
	if err := context.Cause(ctx); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, errors.New("ACME session is closed")
	}
	if s.offset >= len(s.data) {
		return 0, io.EOF
	}
	count := copy(destination, s.data[s.offset:])
	s.offset += count
	if count < len(destination) {
		return count, io.EOF
	}
	return count, nil
}

func (s *memorySession) ReadAt(ctx context.Context, destination []byte, offset int64) (int, error) {
	if err := context.Cause(ctx); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, errors.New("ACME session is closed")
	}
	if offset < 0 || offset >= int64(len(s.data)) {
		return 0, io.EOF
	}
	count := copy(destination, s.data[offset:])
	if count < len(destination) {
		return count, io.EOF
	}
	return count, nil
}

func (s *memorySession) Size(ctx context.Context) (int64, error) {
	if err := context.Cause(ctx); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, errors.New("ACME session is closed")
	}
	return int64(len(s.data)), nil
}

func (s *memorySession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

type sourceOperator struct {
	shape      flow.Shape
	buffers    *buffer.Allocator
	sequential access.Sequential
	random     access.Random
	done       bool
}

func (o *sourceOperator) Ports() flow.Shape { return o.shape.Clone() }
func (*sourceOperator) Close() error        { return nil }

func (o *sourceOperator) Read(ctx context.Context, into *flow.Item[buffer.Handle]) error {
	if o.done {
		return io.EOF
	}
	lease, err := o.buffers.Overwrite(buffer.Spec{Alignment: 1, Planes: []buffer.PlaneSpec{{Size: maxObjectBytes}}})
	if err != nil {
		return err
	}
	defer lease.Discard()
	count := 0
	err = lease.Fill(func(storage buffer.Mutable) error {
		for count < maxObjectBytes {
			var read int
			var readErr error
			if o.sequential != nil {
				read, readErr = o.sequential.Read(ctx, storage.Bytes()[count:])
			} else {
				read, readErr = o.random.ReadAt(ctx, storage.Bytes()[count:], int64(count))
			}
			if read < 0 || read > maxObjectBytes-count {
				return ErrMalformed
			}
			count += read
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			if readErr != nil {
				return readErr
			}
			if read == 0 {
				return io.ErrNoProgress
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if count == 0 {
		o.done = true
		return io.EOF
	}
	full, err := lease.Commit()
	if err != nil {
		return err
	}
	payload := full
	if count != maxObjectBytes {
		payload, err = full.Range(0, count)
		full.Release()
		if err != nil {
			return err
		}
	}
	o.done = true
	*into = flow.NewItem(payload, access.Bytes())
	return nil
}

func mustCapabilities(values ...access.Capability) access.Capabilities {
	capabilities, err := access.NewCapabilities(values...)
	if err != nil {
		panic(err)
	}
	return capabilities
}
