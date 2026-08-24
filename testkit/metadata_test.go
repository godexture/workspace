package testkit

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/plugin"
)

type metadataRunnerPluginID struct{}
type metadataRunnerComponentID struct{}
type metadataRunnerConfigID struct{}
type metadataRunnerCarrierID struct{}

type metadataRunnerConfig struct{}

func TestMetadataRunnerUsesTraitOnlyEncodingAndCancellation(t *testing.T) {
	slot := carrier.Define[metadataRunnerCarrierID]()
	identity := plugin.IdentityOf[metadataRunnerComponentID]()
	var parsed, marshalled, canceled, deadlines atomic.Int32
	configuration := config.Struct[metadataRunnerConfigID](func() metadataRunnerConfig { return metadataRunnerConfig{} }).Version("1").Build()
	component := plugin.NewComponent[metadataRunnerComponentID](plugin.Descriptor{DisplayName: "metadata runner fixture"}, configuration,
		metadata.WithEncoding(
			func(ctx metadata.ParseContext) (metadata.Document, error) {
				parsed.Add(1)
				if err := ctx.Context().Err(); err != nil {
					canceled.Add(1)
					return metadata.Document{}, err
				}
				if _, ok := ctx.Context().Deadline(); ok {
					deadlines.Add(1)
				}
				builder := metadata.NewBuilder(ctx.Scope())
				builder.AddBlock(metadata.NewRawBlock(ctx.Block(), ctx.Carrier(), ctx.Encoding(), ctx.Payload()))
				return builder.Build()
			},
			func(ctx metadata.MarshalContext) (metadata.Blob, []loss.Loss, error) {
				marshalled.Add(1)
				block, ok := ctx.Document().Block(ctx.Block())
				if !ok {
					return metadata.Blob{}, nil, errors.New("metadata raw block is absent")
				}
				return block.Payload(), nil, nil
			},
		),
	)
	definition := plugin.Define[metadataRunnerPluginID](plugin.Descriptor{DisplayName: "metadata runner fixture", Version: "1"}, component)
	payload := metadata.NewBlob("application/octet-stream", []byte{1, 2, 3})
	builder := metadata.NewBuilder(metadata.StreamScope)
	builder.AddBlock(metadata.NewRawBlock("block", slot, identity, payload))
	want, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	coverage := NewCoverage()
	Metadata(t,
		TrackMetadata(MetadataOf(definition, identity), coverage),
		MetadataCase{
			Name:  "raw-roundtrip",
			Input: MetadataInput(slot, "block", metadata.StreamScope, payload),
			Want:  WantMetadata(want, payload),
		},
	)
	coverage.VerifyIdentities(t, plugin.NewSet(definition), identity)
	if parsed.Load() != 4 || marshalled.Load() != 3 || canceled.Load() != 1 || deadlines.Load() != 2 {
		t.Fatalf("Metadata calls = parse %d marshal %d canceled %d deadlines %d", parsed.Load(), marshalled.Load(), canceled.Load(), deadlines.Load())
	}
}

// An encoding panics with a value it chose, which can be the credential it was
// handling. The boundary reports the failure and the stack; the value itself
// must not reach the reported text.
func TestMetadataPanicBoundary(t *testing.T) {
	const secret = "metadata-panic-secret"
	_, _, panicErr := safeMetadataParse(func() (metadata.Document, error) {
		panic(errors.New(secret))
	})
	if panicErr == nil {
		t.Fatal("Metadata Parse panic escaped the testkit boundary")
	}
	if strings.Contains(panicErr.Error(), secret) {
		t.Error("the Metadata Parse report exposes the recovered value")
	}
	_, _, panicErr = safeMetadataMarshal(func() (metadata.Blob, error) {
		panic(errors.New(secret))
	})
	if panicErr == nil {
		t.Fatal("Metadata Marshal panic escaped the testkit boundary")
	}
	if strings.Contains(panicErr.Error(), secret) {
		t.Error("the Metadata Marshal report exposes the recovered value")
	}
}
