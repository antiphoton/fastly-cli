package filesystem

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// FileExists asserts whether a file path exists.
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return !errors.Is(err, fs.ErrNotExist)
}

// CopyFile copies a file from src to dst. If src and dst files exist, and are
// the same, then return successfully. Otherise, attempt to copy the file
// contents from src to dst. The file will be created if it does not already
// exist. If the destination file exists, all it's contents will be replaced by
// the contents of the source file.
func CopyFile(src, dst string) (err error) {
	// Get source file stats.
	ss, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("cannot read source file: %s", src)
	}

	// Assert that source file is regular file.
	if !ss.Mode().IsRegular() {
		// Cannot copy non-regular files (e.g., directories,
		// symlinks, devices, etc.)
		return fmt.Errorf("non-regular source file: %s", src) // #nosec G307
	}

	// Get destination file stats.
	ds, err := os.Stat(dst)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("cannot read destination file: %s", dst)
		}
	} else {
		// Assert that source file is regular file.
		if !ds.Mode().IsRegular() {
			return fmt.Errorf("non-regular destination file: %s", src)
		}

		// If same file, return successfully.
		if os.SameFile(ss, ds) {
			return nil
		}
	}

	// Open source file for reading.
	in, err := os.Open(filepath.Clean(src))
	if err != nil {
		return fmt.Errorf("error reading source file: %w", err)
	}
	defer in.Close() // #nosec G307

	// Create all directories of destination
	if err = os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return fmt.Errorf("creating destination directory: %w", err)
	}

	// Create destination file for writing.
	//
	// gosec flagged this:
	// G304 (CWE-22): Potential file inclusion via variable
	//
	// Disabling as we require a user to configure their own environment.
	/* #nosec */
	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("error creating destination file: %w", err)
	}

	defer func() {
		cerr := out.Close()
		if err == nil {
			err = cerr
		}
	}()

	if _, err = io.Copy(out, in); err != nil {
		return fmt.Errorf("error copying file contents: %w", err)
	}

	return out.Sync()
}

// MakeDirectoryIfNotExists asserts whether a directory exists and makes it
// if not. Returns nil if exists or successfully made.
func MakeDirectoryIfNotExists(path string) error {
	fi, err := os.Stat(path)
	switch {
	case err == nil && fi.IsDir():
		return nil
	case err == nil && !fi.IsDir():
		return fmt.Errorf("%s already exists as a regular file", path)
	case errors.Is(err, fs.ErrNotExist):
		return os.MkdirAll(path, 0o750)
	case err != nil:
		return err
	}

	return nil
}
