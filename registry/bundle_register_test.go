package registry

import (
	"io"
	"reflect"
	"testing"

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

	muxerManifest := MuxerManifest{Factory: func(io.Writer, Configuration) (node.Muxer, error) { return nil, nil }}
	if err := b.Register(testConfig{}, muxerManifest); err != nil {
		t.Fatalf("register muxer: %v", err)
	}

	got, err := b.Muxers.Get(reflect.TypeOf(testConfig{}))
	if err != nil {
		t.Fatalf("get muxer: %v", err)
	}
	if got.Factory == nil {
		t.Fatalf("muxer manifest was not stored")
	}
	if got.ID() != reflect.TypeOf(testConfig{}) {
		t.Fatalf("unexpected muxer id: %v", got.ID())
	}

	demuxerManifest := DemuxerManifest{Factory: func(io.Reader, Configuration) (node.Demuxer, error) { return nil, nil }}
	if err := b.Register(testConfig{}, demuxerManifest); err != nil {
		t.Fatalf("register demuxer: %v", err)
	}

	gotDemuxer, err := b.Demuxers.Get(reflect.TypeOf(testConfig{}))
	if err != nil {
		t.Fatalf("get demuxer: %v", err)
	}
	if gotDemuxer.Factory == nil {
		t.Fatalf("demuxer manifest was not stored")
	}
	if gotDemuxer.ID() != reflect.TypeOf(testConfig{}) {
		t.Fatalf("unexpected demuxer id: %v", gotDemuxer.ID())
	}
}

type unknownManifest struct{}

func (unknownManifest) ID() reflect.Type {
	return reflect.TypeOf(struct{}{})
}

func TestBundleRegisterRejectsUnknownManifest(t *testing.T) {
	t.Parallel()
	b := Bundle{}

	if err := b.Register(testConfig{}, unknownManifest{}); err == nil {
		t.Fatal("expected error for unknown manifest type")
	}
}
