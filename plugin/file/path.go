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
