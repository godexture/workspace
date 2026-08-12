package file

import (
	"errors"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/godexture/godec/access"
)

var errReference = errors.New("file reference is not a local filesystem path")

// Reference converts a platform path to a canonical local file reference.
// Relative paths are resolved at construction time without touching the
// filesystem.
func Reference(path string) (access.Reference, error) {
	if path == "" || strings.IndexByte(path, 0) >= 0 {
		return access.Reference{}, errReference
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return access.Reference{}, errReference
	}
	absolute = filepath.Clean(absolute)
	var value url.URL
	value.Scheme = "file"
	if runtime.GOOS == "windows" && strings.HasPrefix(absolute, `\\`) {
		parts := strings.SplitN(strings.TrimPrefix(filepath.ToSlash(absolute), "//"), "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return access.Reference{}, errReference
		}
		value.Host = parts[0]
		value.Path = "/" + parts[1]
	} else {
		value.Path = filepath.ToSlash(absolute)
		if runtime.GOOS == "windows" && !strings.HasPrefix(value.Path, "/") {
			value.Path = "/" + value.Path
		}
	}
	reference, err := access.Parse(value.String())
	if err != nil {
		return access.Reference{}, errReference
	}
	return reference, nil
}

func pathOf(reference access.Reference) (string, error) {
	if !reference.Valid() || reference.Scheme() != "file" {
		return "", errReference
	}
	parsed, err := url.Parse(reference.Canonical())
	if err != nil || parsed.Scheme != "file" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errReference
	}
	value := parsed.Path
	if value == "" {
		value, err = url.PathUnescape(parsed.Opaque)
	}
	if err != nil || strings.IndexByte(value, 0) >= 0 {
		return "", errReference
	}
	if parsed.Host != "" {
		value = "//" + parsed.Host + "/" + strings.TrimPrefix(value, "/")
	}
	if runtime.GOOS == "windows" && len(value) >= 3 && value[0] == '/' && value[2] == ':' {
		value = value[1:]
	}
	value = filepath.FromSlash(value)
	if value == "" || !filepath.IsAbs(value) {
		return "", errReference
	}
	return filepath.Clean(value), nil
}
