package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// collectDirSize returns the total byte count of all regular files under root.
// Symbolic links are not followed. Returns 0 on any error or empty directory.
func collectDirSize(root string) uint64 {
	var total uint64
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += uint64(info.Size())
		}
		return nil
	})
	return total
}

// dirSlice is the size of one top-level child of a component root.
type dirSlice struct {
	Name  string
	Bytes uint64
}

// dirBreakdown returns the size of each immediate child of root (directories
// recursively summed, files counted individually) and the grand total. Slices
// are sorted largest first. Symlinks are not followed.
func dirBreakdown(root string) (slices []dirSlice, total uint64) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, 0
	}
	for _, e := range entries {
		child := filepath.Join(root, e.Name())
		var size uint64
		if e.IsDir() {
			size = collectDirSize(child)
		} else if info, err := e.Info(); err == nil {
			size = uint64(info.Size())
		}
		total += size
		slices = append(slices, dirSlice{Name: e.Name(), Bytes: size})
	}
	sort.Slice(slices, func(i, j int) bool {
		if slices[i].Bytes != slices[j].Bytes {
			return slices[i].Bytes > slices[j].Bytes
		}
		return slices[i].Name < slices[j].Name
	})
	return slices, total
}
