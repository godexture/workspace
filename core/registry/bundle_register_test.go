package registry

import (
	"io"
	"reflect"
	"testing"

	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
)

type testConfig struct{}

func TestBundleRegisterRoutesByManifestType(t *testing.T) {
	t.Parallel()
	muxerReg := NewRegistry[MuxerManifest]()
	demuxerReg := NewRegistry[DemuxerManifest]()
	encoderReg := NewRegistry[EncoderManifest]()
	decoderReg := NewRegistry[DecoderManifest]()
	filterReg := NewRegistry[FilterManifest]()

	b := Bundle{
		Muxers:   muxerReg,
		Demuxers: demuxerReg,
		Encoders: encoderReg,
		Decoders: decoderReg,
		Filters:  filterReg,
	}

	muxerManifest := MuxerManifest{
		BaseManifest: BaseManifest{Name: "muxer", ConfigurationFactory: StaticConfigurationFactory(testConfig{})},
		Extensions:   []string{".mux"},
		Codecs:       []media.CodecID{media.CodecFLAC},
		DefaultCodec: media.CodecFLAC,
		Factory:      func(io.Writer, Configuration) (node.Muxer, error) { return nil, nil },
	}
	if err := b.Register(muxerManifest); err != nil {
		t.Fatalf("register muxer: %v", err)
	}

	key, err := b.Muxers.Key(testConfig{})
	if err != nil {
		t.Fatalf("derive muxer key: %v", err)
	}
	got, err := b.Muxers.Get(key)
	if err != nil {
		t.Fatalf("get muxer: %v", err)
	}
	if got.Factory == nil {
		t.Fatalf("muxer manifest was not stored")
	}
	if got.ID() != key {
		t.Fatalf("unexpected muxer id: %v", got.ID())
	}

	demuxerManifest := DemuxerManifest{
		BaseManifest: BaseManifest{Name: "demuxer", ConfigurationFactory: StaticConfigurationFactory(testConfig{})},
		Probe:        func(io.Reader) manifest.ProbeScore { return manifest.ProbeExactSignature },
		Factory:      func(io.Reader, Configuration) (node.Demuxer, error) { return nil, nil },
	}
	if err := b.Register(demuxerManifest); err != nil {
		t.Fatalf("register demuxer: %v", err)
	}

	demuxerKey, err := b.Demuxers.Key(testConfig{})
	if err != nil {
		t.Fatalf("derive demuxer key: %v", err)
	}
	gotDemuxer, err := b.Demuxers.Get(demuxerKey)
	if err != nil {
		t.Fatalf("get demuxer: %v", err)
	}
	if gotDemuxer.Factory == nil {
		t.Fatalf("demuxer manifest was not stored")
	}
	if gotDemuxer.ID() != demuxerKey {
		t.Fatalf("unexpected demuxer id: %v", gotDemuxer.ID())
	}
}

type unknownManifest struct{}

func (unknownManifest) ID() PluginKey {
	return PluginKey{}
}

func (unknownManifest) RegistryName() string { return "unknown" }

func (unknownManifest) ConfigurationType() reflect.Type { return reflect.TypeOf(testConfig{}) }

func (unknownManifest) NewConfiguration() (Configuration, error) { return testConfig{}, nil }

func (unknownManifest) Default() Configuration { return testConfig{} }

func TestBundleRegisterRejectsUnknownManifest(t *testing.T) {
	t.Parallel()
	b := Bundle{}

	if err := b.Register(unknownManifest{}); err == nil {
		t.Fatal("expected error for unknown manifest type")
	}
}
