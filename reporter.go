package main

import (
	"os"
	"path/filepath"
	"strings"
)

// ─── Reporter ─────────────────────────────────────────────────────────────────

// Reporter builds a tree-style summary of files saved during a crawl session.
type Reporter struct {
	outputDir string
	included  map[string]bool // absolute paths of session files; nil = show all
	ancDirs   map[string]bool // ancestor dirs of included paths (for tree walk)
}

// NewReporter returns a Reporter scoped to the given output directory that
// shows only the files in sessionPaths. Pass nil to show every file under outputDir.
func NewReporter(outputDir string, sessionPaths []string) *Reporter {
	r := &Reporter{outputDir: outputDir}
	if len(sessionPaths) > 0 {
		r.included = make(map[string]bool, len(sessionPaths))
		r.ancDirs = make(map[string]bool)
		for _, p := range sessionPaths {
			r.included[p] = true
			for dir := filepath.Dir(p); strings.HasPrefix(dir, outputDir) && dir != outputDir; dir = filepath.Dir(dir) {
				r.ancDirs[dir] = true
			}
		}
	}
	return r
}

// Build returns a tree-style string of the session's saved files plus total
// bytes. Returns empty output when nothing was saved.
func (r *Reporter) Build() (tree string, totalBytes int64, err error) {
	if _, statErr := os.Stat(r.outputDir); os.IsNotExist(statErr) {
		return "", 0, nil
	}
	tree = r.treeString()
	if r.included != nil {
		for p := range r.included {
			if info, e := os.Stat(p); e == nil {
				totalBytes += info.Size()
			}
		}
	} else {
		totalBytes, err = r.sumBytes(r.outputDir)
	}
	return
}

// treeLines recursively builds tree lines, filtered to session files when
// r.included is set.
func (r *Reporter) treeLines(dir, prefix string, lines *[]string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	// Filter to session files + their ancestor dirs when a session filter is active.
	if r.included != nil {
		filtered := entries[:0]
		for _, e := range entries {
			abs := filepath.Join(dir, e.Name())
			if e.IsDir() {
				if r.ancDirs[abs] {
					filtered = append(filtered, e)
				}
			} else if r.included[abs] {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
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

// sumBytes returns the total size of all regular files under dir.
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

// treeString builds the tree string, starting from outputDir.
// The root line is omitted — the done-screen already shows it as "Saved to:".
func (r *Reporter) treeString() string {
	var lines []string
	_ = r.treeLines(r.outputDir, "", &lines)
	return strings.Join(lines, "\n")
}
