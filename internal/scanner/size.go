package scanner

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Usage is the measured footprint of a path.
type Usage struct {
	Size   int64
	Files  int64
	Newest time.Time
}

// Measure sums the size of regular files under path (or of path itself when
// it is a file). Symlinks are never followed. Unreadable entries are skipped
// and the first such error is returned alongside the partial Usage; a context
// cancellation aborts and returns ctx's error.
func Measure(ctx context.Context, path string) (Usage, error) {
	var u Usage
	info, err := os.Lstat(path)
	if err != nil {
		return u, err
	}
	if !info.IsDir() {
		if info.Mode().IsRegular() {
			u.Size, u.Files, u.Newest = info.Size(), 1, info.ModTime()
		}
		return u, nil
	}

	var first error
	note := func(err error) {
		if first == nil {
			first = err
		}
	}
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if err != nil {
			note(err)
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		fi, ierr := d.Info()
		if ierr != nil {
			note(ierr)
			return nil
		}
		if !fi.Mode().IsRegular() {
			return nil
		}
		u.Files++
		u.Size += fi.Size()
		if fi.ModTime().After(u.Newest) {
			u.Newest = fi.ModTime()
		}
		return nil
	})
	if err != nil {
		return u, err
	}
	return u, first
}
