// Copyright (c) the go-ruby-zeitwerk/zeitwerk authors
//
// SPDX-License-Identifier: BSD-3-Clause

package zeitwerk

import (
	"os"
	"sort"
)

// DirEntry is one child of a directory as reported by an FS: its basename and
// whether it is itself a directory. It is the minimal shape the loader's
// directory scan needs.
type DirEntry struct {
	Name  string
	IsDir bool
}

// FS is the filesystem seam the loader scans. The default implementation reads
// the real filesystem, so PushDir takes ordinary absolute paths; tests (and any
// host that manages a virtual tree) inject their own by calling SetFS. Only
// directory listing is required — the loader never reads file contents itself,
// since loading a file is the host's Load seam.
type FS interface {
	// ReadDir lists the children of dir. The returned entries need not be
	// sorted; the loader orders constants deterministically itself.
	ReadDir(dir string) ([]DirEntry, error)
}

// osFS is the default FS, backed by the real filesystem via os.ReadDir.
type osFS struct{}

func (osFS) ReadDir(dir string) ([]DirEntry, error) {
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	entries := make([]DirEntry, len(des))
	for i, de := range des {
		entries[i] = DirEntry{Name: de.Name(), IsDir: de.IsDir()}
	}
	return entries, nil
}

// sortedEntries returns es ordered by name, so a scan over any FS yields a
// deterministic constant order regardless of the FS's native listing order.
func sortedEntries(es []DirEntry) []DirEntry {
	out := append([]DirEntry(nil), es...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
