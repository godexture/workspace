package file

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/godexture/godec/access"
)

func TestRedactIOHidesFilesystemNamesFromEveryFmtRepresentation(t *testing.T) {
	if got := redactIO("read", io.EOF); got != io.EOF {
		t.Fatalf("redactIO changed EOF identity: %#v", got)
	}
	secret := filepath.Join(t.TempDir(), "private", "missing.wav")
	raw := &os.PathError{Op: "open", Path: secret, Err: os.ErrNotExist}
	err := redactIO("open", raw)
	for name, value := range map[string]string{
		"error":   err.Error(),
		"v":       fmt.Sprintf("%v", err),
		"plus-v":  fmt.Sprintf("%+v", err),
		"sharp-v": fmt.Sprintf("%#v", err),
	} {
		if strings.Contains(value, secret) || strings.Contains(value, filepath.Base(secret)) {
			t.Fatalf("%s representation leaked %q: %q", name, secret, value)
		}
	}
	if !strings.Contains(err.Error(), "file open failed") || !strings.Contains(err.Error(), "not-found") {
		t.Fatalf("redacted error = %q", err)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("errors.Is lost os.ErrNotExist: %v", err)
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		t.Fatalf("errors.As exposed the private PathError: %#v", pathErr)
	}
}

func TestAcquireSourceRedactsMissingSecretPath(t *testing.T) {
	secret := filepath.Join(t.TempDir(), "credentials", "private.wav")
	_, err := acquireSource(t.Context(), fileReference(t, secret), selection(t, sourceCapabilities(), access.RandomRead))
	if err == nil {
		t.Fatal("missing source unexpectedly acquired")
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), filepath.Base(secret)) {
		t.Fatalf("source acquire leaked path: %v", err)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source acquire lost not-exist identity: %v", err)
	}
}
