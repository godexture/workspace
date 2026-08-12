package file

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/diagnostic"
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

// ValidateDistinct rejects file-to-file operations whose input and output
// identify the same file. In-place replacement requires a separate explicit
// contract because a successful commit destroys the original input.
func ValidateDistinct(inputPath, outputPath string) error {
	input, err := absolutePath(inputPath)
	if err != nil {
		return err
	}
	output, err := absolutePath(outputPath)
	if err != nil {
		return err
	}
	outputInfo, err := os.Stat(output)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if equalPath(input, output) {
			return sameFileDiagnostic()
		}
		return nil
	}
	inputInfo, err := os.Stat(input)
	if err != nil {
		return err
	}
	if os.SameFile(inputInfo, outputInfo) {
		return sameFileDiagnostic()
	}
	return nil
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

func sameFileDiagnostic() error {
	return diagnostic.NewError(diagnostic.NewItem(
		"file.same-path",
		diagnostic.ErrorSeverity,
		diagnostic.Path{},
		"input and output identify the same file; in-place conversion is not enabled",
		nil,
	))
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
