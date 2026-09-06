package safe

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	DirMode  os.FileMode = 0o700
	FileMode os.FileMode = 0o600
)

// MkdirSecure creates a directory tree owned only by the user.
func MkdirSecure(path string) error {
	if err := os.MkdirAll(path, DirMode); err != nil {
		return err
	}
	// MkdirAll honours umask, so an inherited umask can widen the mode. Tighten
	// the leaf explicitly.
	return os.Chmod(path, DirMode)
}

// WriteFileSecure writes data with its final permissions applied at creation
// time.
//
// The obvious implementation — write, then chmod — leaves a window in which the
// file is world-readable, which matters because these files hold OAuth client
// details and, on the file backend, sealed secrets (SEC-04). Creating the
// temporary file with O_EXCL at 0600 and renaming closes that window, and makes
// the replacement atomic.
func WriteFileSecure(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := MkdirSecure(dir); err != nil {
		return err
	}
	tmp, err := os.OpenFile(
		filepath.Join(dir, "."+filepath.Base(path)+".tmp"),
		os.O_WRONLY|os.O_CREATE|os.O_EXCL|os.O_TRUNC,
		FileMode,
	)
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	name := tmp.Name()
	defer os.Remove(name)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
