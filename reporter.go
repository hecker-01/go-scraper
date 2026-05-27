package main

import (
	"os"
	"path/filepath"
	"strings"
)

// ─── Reporter ─────────────────────────────────────────────────────────────────

// Reporter walks the output directory after a crawl and produces a tree-style
// summary string along with the total number of bytes downloaded.
type Reporter struct {
	outputDir string
}

// NewReporter returns a Reporter for the given output directory.
func NewReporter(outputDir string) *Reporter {
	return &Reporter{outputDir: outputDir}
}

// Build walks outputDir and returns:
//   - a tree-style string (like the Linux `tree` command) of every saved file
//   - the total bytes across all files
//   - any error encountered while walking
func (r *Reporter) Build() (tree string, totalBytes int64, err error) {
	// TODO (Phase 8): implement tree walk
	// - filepath.WalkDir(r.outputDir, ...)
	// - accumulate sizes with fi.Size()
	// - build tree string using box-drawing characters
	//   (not dashes - use  |, L, └, ├, │ etc.)
	_ = r.outputDir
	return "", 0, nil
}

// treeLines is a helper for building the tree string recursively.
// prefix is the current indentation (e.g. "│   ").
func (r *Reporter) treeLines(dir, prefix string, lines *[]string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for i, e := range entries {
		connector := "├── "
		childPrefix := prefix + "│   "
		if i == len(entries)-1 {
			connector = "└── "
			childPrefix = prefix + "    "
		}
		*lines = append(*lines, prefix+connector+e.Name())
		if e.IsDir() {
			if err := r.treeLines(filepath.Join(dir, e.Name()), childPrefix, lines); err != nil {
				return err
			}
		}
	}
	return nil
}

// sumBytes returns the total size in bytes of all regular files under dir.
func (r *Reporter) sumBytes(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	return total, err
}

// treeString builds the full tree string for outputDir.
func (r *Reporter) treeString() string {
	var lines []string
	lines = append(lines, r.outputDir)
	_ = r.treeLines(r.outputDir, "", &lines)
	return strings.Join(lines, "\n")
}
