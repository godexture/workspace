package file

import (
	"context"
	"errors"
	"net/url"
	"os"
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
	absolute, err := absolutePath(path)
	if err != nil {
		return access.Reference{}, err
	}
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

// equivalent reports whether two file references denote one file. Names that
// differ can still name the same object through a link or a case-insensitive
// volume, which only the filesystem can resolve; a target that does not exist
// yet is compared by normalized path.
func equivalent(_ context.Context, target, other access.Reference) (bool, error) {
	left, err := pathOf(target)
	if err != nil {
		return false, err
	}
	right, err := pathOf(other)
	if err != nil {
		return false, err
	}
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	if errors.Is(leftErr, os.ErrNotExist) || errors.Is(rightErr, os.ErrNotExist) {
		return equalPath(left, right), nil
	}
	if leftErr != nil {
		return false, leftErr
	}
	if rightErr != nil {
		return false, rightErr
	}
	return os.SameFile(leftInfo, rightInfo), nil
}

func absolutePath(path string) (string, error) {
	if path == "" || strings.IndexByte(path, 0) >= 0 {
		return "", errReference
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", errReference
	}
	return filepath.Clean(absolute), nil
}

func equalPath(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
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
