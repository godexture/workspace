package routing

import (
	"context"
	"fmt"
	"io"

	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
	"github.com/godexture/core/resolver"
)

type mockNode struct {
	onClose func()
}

func (m *mockNode) Start(ctx context.Context) error { return nil }
func (m *mockNode) Close() error {
	if m.onClose != nil {
		m.onClose()
	}
	return nil
}

type mockDemuxer struct {
	mockNode
	streams  []media.StreamInfo
	metadata *metadata.Bundle
}

func (m *mockDemuxer) Metadata() *metadata.Bundle                           { return m.metadata }
func (m *mockDemuxer) Streams() ([]media.StreamInfo, error)                 { return m.streams, nil }
func (m *mockDemuxer) OutputPorts() map[string]*node.OutPort[*media.Packet] { return nil }

type mockDecoder struct {
	mockNode
}

func (m *mockDecoder) InputPorts() map[string]*node.InPort[*media.Packet] { return nil }
func (m *mockDecoder) OutputPorts() map[string]*node.OutPort[media.Frame] { return nil }

type mockEncoder struct {
	mockNode
}

func (m *mockEncoder) InputPorts() map[string]*node.InPort[media.Frame]     { return nil }
func (m *mockEncoder) OutputPorts() map[string]*node.OutPort[*media.Packet] { return nil }

type mockFilter struct {
	mockNode
	inputs map[string]*node.InPort[media.Frame]
}

func (m *mockFilter) Process(context.Context) error                      { return nil }
func (m *mockFilter) InputPorts() map[string]*node.InPort[media.Frame]   { return m.inputs }
func (m *mockFilter) OutputPorts() map[string]*node.OutPort[media.Frame] { return nil }

type mockMuxer struct {
	mockNode
	addedStreams []media.StreamInfo
	metadata     *metadata.Bundle
}

func (m *mockMuxer) AddStream(info media.StreamInfo) (int, error) {
	m.addedStreams = append(m.addedStreams, info)
	return len(m.addedStreams) - 1, nil
}
func (m *mockMuxer) SetMetadata(meta *metadata.Bundle) error            { m.metadata = meta; return nil }
func (m *mockMuxer) InputPorts() map[string]*node.InPort[*media.Packet] { return nil }

// Mock Resolvers
type mockDemuxerResolver struct {
	resolved registry.DemuxerManifest
	called   bool
}

func (r *mockDemuxerResolver) ResolveDemuxer(stream io.ReadSeeker, opts ...resolver.Option) (registry.DemuxerManifest, error) {
	r.called = true
	return r.resolved, nil
}

type mockDecoderResolver struct {
	resolved registry.DecoderManifest
	called   bool
}

func (r *mockDecoderResolver) ResolveDecoder(stream media.StreamInfo, opts ...resolver.Option) (registry.DecoderManifest, error) {
	r.called = true
	return r.resolved, nil
}

type mockEncoderResolver struct {
	resolved registry.EncoderManifest
	called   bool
}

func (r *mockEncoderResolver) ResolveEncoder(codec media.CodecID, opts ...resolver.Option) (registry.EncoderManifest, error) {
	r.called = true
	return r.resolved, nil
}

type mockMuxerResolver struct {
	resolved registry.MuxerManifest
	called   bool
	err      error
}

func (r *mockMuxerResolver) ResolveMuxer(config registry.Configuration) (registry.MuxerManifest, error) {
	r.called = true
	if r.err != nil {
		return registry.MuxerManifest{}, r.err
	}
	return r.resolved, nil
}

type mockFilterResolver struct {
	resolved []registry.FilterManifest
	configs  []registry.Configuration
}

type mockBridgeResolver struct {
	steps  []resolver.BridgeStep
	called bool
}

func (r *mockBridgeResolver) ResolveBridge(media.StreamInfo, []manifest.Capability) ([]resolver.BridgeStep, error) {
	r.called = true
	return r.steps, nil
}

func (r *mockFilterResolver) ResolveFilter(config registry.Configuration) (registry.FilterManifest, error) {
	r.configs = append(r.configs, config)
	index := len(r.configs) - 1
	if index >= len(r.resolved) {
		return registry.FilterManifest{}, fmt.Errorf("unexpected filter %d", index)
	}
	return r.resolved[index], nil
}

type dummyConfig struct{}
type firstFilterConfig struct{}
type secondFilterConfig struct{}
type auxFilterConfig struct{}

type alwaysCapability struct{}

func (alwaysCapability) Match(media.StreamInfo) bool     { return true }
func (alwaysCapability) Diagnose(media.StreamInfo) error { return nil }
