package file

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
)

const temporaryAttempts = 100

func createTemporary(target string) (*os.File, error) {
	directory := filepath.Dir(target)
	prefix := "." + filepath.Base(target) + ".godec-"
	for range temporaryAttempts {
		var token [8]byte
		if _, err := rand.Read(token[:]); err != nil {
			return nil, err
		}
		path := filepath.Join(directory, prefix+hex.EncodeToString(token[:]))
		handle, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o666)
		if err == nil {
			return handle, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, redactIO("open-temp", err)
		}
	}
	return nil, errors.New("file: could not allocate a unique temporary file")
}

func preservePermissions(temporary, target string) error {
	info, err := os.Stat(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return redactIO("stat", err)
	}
	return redactIO("chmod", os.Chmod(temporary, info.Mode().Perm()))
}
