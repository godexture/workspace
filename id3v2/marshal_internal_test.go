package id3v2

import (
	"bytes"
	"testing"
)

func TestEncoder_addURLFrame(t *testing.T) {
	t.Parallel()
	e := &encoder{opts: MarshalOptions{Version: Version3}}
	e.addURLFrame("WOAR", "https://example.com")
	if len(e.frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(e.frames))
	}
	frame := e.frames[0]
	// Frame Header: ID(4) + Size(4) + Flags(2) = 10 bytes
	if !bytes.HasPrefix(frame, []byte("WOAR")) {
		t.Errorf("frame ID = %q, want WOAR", frame[:4])
	}
	// skip size and flags
	payload := frame[10:]
	if string(payload) != "https://example.com" {
		t.Errorf("payload = %q, want https://example.com", payload)
	}
}

func TestEncoder_addRawAttachedPictureFrame(t *testing.T) {
	t.Parallel()
	e := &encoder{opts: MarshalOptions{Version: Version3}}
	
	// Create a dummy APIC payload
	payload := []byte{0x00, 'i', 'm', 'a', 'g', 'e', '/', 'p', 'n', 'g', 0x00, 0x03, 'C', 'o', 'v', 'e', 'r', 0x00, 0x89, 'P', 'N', 'G'}
	
	e.addRawAttachedPictureFrame(payload)
	if len(e.frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(e.frames))
	}
	
	if !bytes.HasPrefix(e.frames[0], []byte("APIC")) {
		t.Errorf("frame ID = %q, want APIC", e.frames[0][:4])
	}
}

func TestEncoder_addRawAttachedPictureFrame_V2(t *testing.T) {
	t.Parallel()
	e := &encoder{opts: MarshalOptions{Version: Version2}}
	
	// Create a dummy APIC payload
	payload := []byte{0x00, 'i', 'm', 'a', 'g', 'e', '/', 'p', 'n', 'g', 0x00, 0x03, 'C', 'o', 'v', 'e', 'r', 0x00, 0x89, 'P', 'N', 'G'}
	
	e.addRawAttachedPictureFrame(payload)
	if len(e.frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(e.frames))
	}
	
	if !bytes.HasPrefix(e.frames[0], []byte("PIC")) {
		t.Errorf("frame ID = %q, want PIC", e.frames[0][:3]) // PIC is 3 bytes in V2
	}
}
