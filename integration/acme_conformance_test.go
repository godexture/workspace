package integration_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/integration/acme"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/codec"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/standard"
	"github.com/godexture/godec/testkit"
)

func TestThirdPartyPluginConformance(t *testing.T) {
	definition := acme.Plugin()
	set := standard.Set().Add(definition)
	coverage := testkit.NewCoverage()
	label := "outside-core"
	payload := []byte{0, 4, 254}
	encoded, err := acme.Encode(label, payload)
	if err != nil {
		t.Fatal(err)
	}

	testkit.Plugin(t, definition)
	testkit.Access(t,
		testkit.TrackAccess(testkit.AccessIn(set, acme.SourceIdentity()), coverage),
		testkit.AccessCase{
			Name:  "reference-backed-random-read",
			Input: acmeAccessFixture(t, encoded),
			Want: testkit.WantAccess(encoded,
				access.AnyOf(access.RandomRead, access.StableSize),
				access.AnyOf(access.SequentialRead),
			),
		},
	)
	testkit.Format(t,
		testkit.Track(testkit.SubjectIn(set, acme.ReaderIdentity(), "bytes", access.Bytes(), "packets", codec.Packets()), coverage),
		testkit.Case[buffer.Handle, packet.Packet]{
			Name:  "inspect-label-and-demux-payload",
			Input: testkit.ByteInput(encoded),
			Want: testkit.WantPackets(testkit.Packet{
				Sequence: 0, PTS: timing.UnknownPTS(), DTS: timing.UnknownDTS(), Duration: timing.UnknownDuration(), Bytes: payload,
			}),
		},
		testkit.Case[buffer.Handle, packet.Packet]{
			Name:  "missing-payload",
			Input: testkit.ByteInput([]byte{'A', 'C', 'M', '1', 1, 'x'}),
			Want:  testkit.WantPlanError[packet.Packet](acme.ErrMalformed),
		},
	)
	document := acmeDocument(t, label, true)
	testkit.Codec(t,
		testkit.Track(testkit.SubjectIn(set, acme.DecoderIdentity(), "packets", codec.Packets(), "values", acme.Values()), coverage),
		testkit.Case[packet.Packet, acme.Value]{
			Name: "increment-payload",
			Input: testkit.PacketInputFor(acmePacketDescriptor(t, document), []testkit.Packet{{
				Sequence: 0, PTS: timing.UnknownPTS(), DTS: timing.UnknownDTS(), Duration: timing.UnknownDuration(), Bytes: payload,
			}}),
			Want: testkit.EqualValues(acme.Value{Number: 1}, acme.Value{Number: 5}, acme.Value{Number: 255}),
		},
	)
	testkit.Format(t,
		testkit.Track(testkit.SubjectIn(set, acme.WriterIdentity(), "values", acme.Values(), "writes", access.Writes()), coverage),
		testkit.Case[acme.Value, access.Write]{
			Name:  "marshal-label-and-mux-values",
			Input: testkit.Values(acmeValueDescriptor(acmeDocument(t, label, false)), acme.Values(), acme.Value{Number: 1}, acme.Value{Number: 5}, acme.Value{Number: 255}),
			Want:  testkit.WantWriteImage(mustACME(t, label, []byte{1, 5, 255})),
		},
		testkit.Case[acme.Value, access.Write]{
			Name:  "empty-values",
			Input: testkit.Values(acmeValueDescriptor(acmeDocument(t, label, false)), acme.Values()),
			Want:  testkit.WantRunError[access.Write](acme.ErrMalformed),
		},
	)
	metadataPayload := metadata.NewBlob("text/plain; charset=utf-8", []byte(label))
	testkit.Metadata(t,
		testkit.TrackMetadata(testkit.MetadataIn(set, acme.EncodingIdentity()), coverage),
		testkit.MetadataCase{
			Name:  "label-roundtrip",
			Input: testkit.MetadataInput(acme.LabelCarrier(), "acme/label", metadata.StreamScope, metadataPayload),
			Want:  testkit.WantMetadata(document, metadataPayload),
		},
	)
	coverage.VerifyExecutable(t, plugin.NewSet(definition))
	coverage.VerifyIdentities(t, plugin.NewSet(definition), acme.EncodingIdentity())
}

func acmeAccessFixture(t testing.TB, encoded []byte) testkit.AccessFixture {
	t.Helper()
	reference, err := acme.Reference(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return testkit.AccessFixtureOf(encoded, func(context.Context) (testkit.AccessTarget, error) {
		return testkit.NewAccessTarget(
			reference,
			func(_ context.Context, value []byte) error {
				if !bytes.Equal(value, encoded) {
					return errors.New("ACME fixture seed changed")
				}
				return nil
			},
			func(context.Context) ([]byte, error) { return append([]byte(nil), encoded...), nil },
			func(context.Context) ([]string, error) { return nil, nil },
			func() error { return nil },
		), nil
	})
}

func acmeDocument(t testing.TB, label string, raw bool) metadata.Document {
	t.Helper()
	builder := metadata.NewBuilder(metadata.StreamScope)
	origin := metadata.Origin{}
	if raw {
		block := metadata.BlockID("acme/label")
		blob := metadata.NewBlob("text/plain; charset=utf-8", []byte(label))
		builder.AddBlock(metadata.NewRawBlock(block, acme.LabelCarrier(), acme.EncodingIdentity(), blob))
		origin = metadata.Origin{Encoding: acme.EncodingIdentity(), Carrier: acme.LabelCarrier(), Block: block, Native: "label"}
	}
	metadata.Add(builder, acme.Label(), label, origin)
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func acmePacketDescriptor(t testing.TB, document metadata.Document) stream.Descriptor {
	t.Helper()
	properties, err := codec.WithTag(property.New(), acme.CodecTag())
	if err != nil {
		t.Fatal(err)
	}
	return stream.MustDescriptor("acme", codec.Packets().Identity(), timing.MustBase(1, 1), properties).WithMetadata(document)
}

func acmeValueDescriptor(document metadata.Document) stream.Descriptor {
	return stream.MustDescriptor("acme", acme.Values().Identity(), timing.MustBase(1, 1), property.New()).WithMetadata(document)
}

func mustACME(t testing.TB, label string, payload []byte) []byte {
	t.Helper()
	value, err := acme.Encode(label, payload)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
