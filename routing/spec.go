package routing

import (
	"fmt"
	"io"
	"reflect"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/registry"
)

// MainInputAlias is the reserved source alias for the main input's decoded
// stream. It cannot be used as an auxiliary input name or a filter alias.
const MainInputAlias = "@in"

// PortRef names one port of one graph node: a filter alias, an auxiliary
// input name, or MainInputAlias. An empty Port defaults to "out".
type PortRef struct {
	Alias string
	Port  string
}

// FilterSpec describes one filter node in the conversion graph. Inputs maps
// each port this filter's manifest requires to where it reads from. A port
// left out of Inputs falls back to a default only when it is literally named
// "in": the first filter in declaration order reads MainInputAlias, and
// every later one reads the "out" port of the filter declared immediately
// before it. Every other port (multi-port filters have no "in" at all) must
// be wired explicitly.
type FilterSpec struct {
	Alias  string
	Config registry.Configuration
	Inputs map[string]PortRef
	// Manifest is the filter's already-resolved manifest, when the caller
	// has one (as pkg/conversion does, since it must resolve a
	// parameterized filter's manifest from its Parameters before Config
	// even makes sense). A parameterized filter's concrete manifest exists
	// only for this one invocation, so it cannot be found again by
	// Negotiator's config-keyed filter resolver the way an ordinary
	// filter's can — leave this zero to fall back to that resolver.
	Manifest registry.FilterManifest
}

// AuxInputSpec is a named additional source, demuxed and decoded exactly
// like the main input. Filters read it by wiring a port to PortRef{Alias:
// name}; it has no filter chain of its own — any processing on the way to a
// consumer is just an ordinary FilterSpec wired from this alias, unified
// with the rest of the graph.
type AuxInputSpec struct {
	Source io.ReadSeeker

	DemuxManifest registry.DemuxerManifest
	DemuxConfig   registry.Configuration

	DecoderManifest registry.DecoderManifest
	DecodeConfig    registry.Configuration
}

type ConversionSpec struct {
	Input  io.ReadSeeker
	Output io.Writer

	DemuxManifest     registry.DemuxerManifest
	DemuxConfig       registry.Configuration
	SelectInputStream func(streams []media.StreamInfo) (media.StreamInfo, error)

	DecoderManifest registry.DecoderManifest
	DecodeConfig    registry.Configuration
	Filters         []FilterSpec
	AuxInputs       map[string]AuxInputSpec

	// Sink names the port that feeds the encoder. Nil resolves to the
	// default: the last filter's "out" port, or (with no filters) the main
	// input directly.
	Sink *PortRef

	EncoderManifest registry.EncoderManifest
	TargetCodec     media.CodecID
	EncodeConfig    registry.Configuration

	MuxManifest         registry.MuxerManifest
	MuxConfig           registry.Configuration
	PrepareOutputStream func(inStream media.StreamInfo) media.StreamInfo

	// Resources sets the total execution budget. Parallelism == 0 uses
	// runtime.GOMAXPROCS(0).
	Resources registry.ResourceBudget
}

func configurationFor(manifest registry.Manifest, requested registry.Configuration) (registry.Configuration, error) {
	if requested == nil {
		if manifest.ID().ConfigurationType() == nil {
			return nil, nil
		}
		return manifest.NewConfiguration()
	}
	actual := reflect.TypeOf(requested)
	value := reflect.ValueOf(requested)
	if value.Kind() == reflect.Pointer && value.IsNil() {
		return nil, fmt.Errorf("configuration must not be a nil pointer")
	}
	for actual.Kind() == reflect.Pointer {
		actual = actual.Elem()
	}
	expected := manifest.ID().ConfigurationType()
	if expected == nil {
		return requested, nil
	}
	if actual != expected {
		return nil, fmt.Errorf("configuration type %s does not match %s", actual, manifest.ID())
	}
	return requested, nil
}
