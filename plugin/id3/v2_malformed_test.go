package id3

import (
	"errors"
	"testing"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/tag"
)

func TestV2RejectsStructurallyAmbiguousTags(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	valid := v2TestTag(v2BuildFrame("TIT2", []byte{3, 'T'}))
	overrun := append([]byte(nil), valid...)
	overrun[9]++
	paddingThenFrame := v2TestTag(append(make([]byte, 10), v2BuildFrame("TIT2", []byte{3, 'T'})...))
	badFooter := append([]byte{'I', 'D', '3', 4, 0, 0x10, 0, 0, 0, 0}, []byte{'3', 'D', 'I', 3, 0, 0x10, 0, 0, 0, 0}...)
	for _, value := range [][]byte{
		[]byte("ID3\x04\x00\x00\x80\x00\x00\x00"),
		overrun,
		paddingThenFrame,
		badFooter,
		v2TestTagVersion(4, 0x40, []byte{0, 0, 0, 5, 1}),
	} {
		if _, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, value)); !errors.Is(err, errV2Malformed) {
			t.Fatalf("malformed ID3v2 error = %v for %x", err, value)
		}
	}
}

func TestV2RetainsBoundarySafeMalformedFrameOpaque(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	frame := v2BuildFrame("xAAA", []byte{1, 2, 3})
	payload := v2TestTag(frame)
	document, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, payload))
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Entries()) != 0 || len(document.Blocks()) != 2 || document.Blocks()[1].Source() {
		t.Fatalf("boundary-safe malformed frame = %#v", document)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "head", document)
	if err != nil || len(reports) != 0 || string(encoded.AppendTo(nil)) != string(payload) {
		t.Fatalf("boundary-safe malformed roundtrip = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}
}

func TestV2CanonicalEditOmitsSafeExtendedHeadersAndRejectsRestrictions(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	title := v2BuildFrame("TIT2", []byte{3, 'T'})
	for _, test := range []struct {
		name         string
		version      byte
		extended     []byte
		restrictions bool
	}{
		{name: "v2.3", version: 3, extended: []byte{0, 0, 0, 6, 0, 0, 0, 0, 0, 0}},
		{name: "v2.4", version: 4, extended: []byte{0, 0, 0, 6, 1, 0}},
		{name: "v2.4 restrictions", version: 4, extended: []byte{0, 0, 0, 8, 1, 0x10, 1, 0}, restrictions: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			frame := title
			if test.version == 3 {
				frame = v2TestFrame(3, "TIT2", []byte{0, 'T'}, [2]byte{})
			}
			payload := v2TestTagVersion(test.version, 0x40, append(test.extended, frame...))
			document, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, payload))
			if err != nil {
				t.Fatal(err)
			}
			builder := document.Edit()
			metadata.Add(builder, tag.Title(), "Edited", metadata.Origin{})
			edited, err := builder.Build()
			if err != nil {
				t.Fatal(err)
			}
			encoded, _, err := resolver.Marshal(t.Context(), slot, "head", edited)
			if test.restrictions {
				if !errors.Is(err, errV2Unsupported) {
					t.Fatalf("restricted edit error = %v", err)
				}
				return
			}
			if err != nil || len(encoded.AppendTo(nil)) < 6 || encoded.AppendTo(nil)[5] != 0 {
				t.Fatalf("canonical extended-header edit = %x, error %v", encoded.AppendTo(nil), err)
			}
		})
	}
}

func TestV2AcceptsTrailingZeroPaddingExceptWithFooter(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	frame := v2BuildFrame("TIT2", []byte{3, 'T'})
	padded := v2TestTag(append(frame, make([]byte, 3)...))
	document, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, padded))
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "head", document)
	if err != nil || len(reports) != 0 || string(encoded.AppendTo(nil)) != string(padded) {
		t.Fatalf("padded tag roundtrip = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}
	footerBody := append(append([]byte(nil), frame...), 0)
	footer := append([]byte{'I', 'D', '3', 4, 0, 0x10}, v2EncodeSyncSafe(len(footerBody))...)
	footer = append(footer, footerBody...)
	footer = append(footer, '3', 'D', 'I', 4, 0, 0x10)
	footer = append(footer, v2EncodeSyncSafe(len(footerBody))...)
	for _, malformed := range [][]byte{footer, v2TestTag(append(frame, 1))} {
		if _, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, malformed)); !errors.Is(err, errV2Malformed) {
			t.Fatalf("padding error = %v for %x", err, malformed)
		}
	}
}

func TestV2RejectsMalformedExtendedHeaderPaddingAndCRC(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	titleV23 := v2TestFrame(3, "TIT2", []byte{0, 'T'}, [2]byte{})
	titleV24 := v2BuildFrame("TIT2", []byte{3, 'T'})
	for _, payload := range [][]byte{
		v2TestTagVersion(3, 0x40, append([]byte{0, 0, 0, 6, 0, 0, 0, 0, 0, 1}, titleV23...)),
		v2TestTagVersion(4, 0x40, append([]byte{0, 0, 0, 12, 1, 0x20, 5, 0x80, 0, 0, 0, 0}, titleV24...)),
		v2TestTagVersion(4, 0x40, append([]byte{0, 0, 0, 12, 1, 0x20, 5, 0x10, 0, 0, 0, 0}, titleV24...)),
	} {
		if _, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, payload)); !errors.Is(err, errV2Malformed) {
			t.Fatalf("malformed extended header error = %v for %x", err, payload)
		}
	}
}

func TestV23ExtendedHeaderRejectsUnderDeclaredPadding(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	frame := v2TestFrame(3, "TIT2", []byte{0, 'T', 0}, [2]byte{})
	extended := []byte{0, 0, 0, 6, 0, 0, 0, 0, 0, 2}
	payload := v2TestTagVersion(3, 0x40, append(append(extended, frame...), 0, 0, 0, 0))
	if _, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, payload)); !errors.Is(err, errV2Malformed) {
		t.Fatalf("under-declared padding error = %v for %x", err, payload)
	}
}
