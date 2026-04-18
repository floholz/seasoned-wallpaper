// Package picker performs random wallpaper selection with bounded no-repeat
// memory. Pure logic aside from a single os.Stat/ReadDir per call — safe to
// test against a tmpfs fixture.
package picker

import (
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Result describes a picked wallpaper.
type Result struct {
	// Path is the absolute path of the chosen file.
	Path string
	// Source is the folder used as the pool for no-repeat tracking. When the
	// input was a direct file, Source equals Path.
	Source string
	// Pool is the total number of matching files considered before exclusion.
	Pool int
}

// Pick chooses a wallpaper from src.
//
//   - If src is a regular file, it is returned as-is (extension filter is
//     not enforced — a user who pointed a season directly at a file meant it).
//   - If src is a directory, files matching exts are scanned (recursive
//     when true), and a uniformly-random file is chosen, excluding the first
//     N entries of recent, where N = min(5, floor(pool/2)). If that leaves
//     zero candidates the exclusion set is dropped and a pick is made from
//     the full pool.
//
// recent entries are interpreted as paths relative to src (for non-recursive
// scans this reduces to basenames).
func Pick(src string, recursive bool, exts []string, recent []string, rng *rand.Rand) (Result, error) {
	info, err := os.Stat(src)
	if err != nil {
		return Result{}, fmt.Errorf("picker: stat %s: %w", src, err)
	}
	if !info.IsDir() {
		return Result{Path: src, Source: src, Pool: 1}, nil
	}

	files, err := scanDir(src, recursive, exts)
	if err != nil {
		return Result{}, err
	}
	if len(files) == 0 {
		return Result{}, fmt.Errorf("picker: no matching files under %s", src)
	}

	excludeN := len(files) / 2
	if excludeN > 5 {
		excludeN = 5
	}
	excluded := make(map[string]bool, excludeN)
	for i := 0; i < len(recent) && i < excludeN; i++ {
		excluded[recent[i]] = true
	}

	candidates := make([]string, 0, len(files))
	for _, abs := range files {
		if !excluded[RelKey(src, abs)] {
			candidates = append(candidates, abs)
		}
	}
	if len(candidates) == 0 {
		candidates = files
	}
	pick := candidates[rng.IntN(len(candidates))]
	return Result{Path: pick, Source: src, Pool: len(files)}, nil
}

// RelKey returns the key used for recent-entry tracking: the path of abs
// relative to src, with forward-slash separators for cross-platform state
// files. For a file src (or unrelated abs), the basename is returned.
func RelKey(src, abs string) string {
	rel, err := filepath.Rel(src, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.Base(abs)
	}
	return filepath.ToSlash(rel)
}

// Trim shortens recent to the current allowed exclusion size for a pool of
// the given length, adding latest at the front and dropping anything older.
func Trim(recent []string, latest string, poolSize int) []string {
	limit := poolSize / 2
	if limit > 5 {
		limit = 5
	}
	if limit < 1 {
		limit = 1
	}
	out := make([]string, 0, limit)
	out = append(out, latest)
	for _, r := range recent {
		if r == latest {
			continue
		}
		if len(out) >= limit {
			break
		}
		out = append(out, r)
	}
	return out
}

// scanDir returns matching files beneath root. Results are sorted so callers
// get deterministic ordering independent of filesystem readdir order.
func scanDir(root string, recursive bool, exts []string) ([]string, error) {
	extSet := make(map[string]bool, len(exts))
	for _, e := range exts {
		extSet[strings.ToLower(strings.TrimPrefix(e, "."))] = true
	}
	var out []string
	if recursive {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if extSet[fileExt(path)] {
				out = append(out, path)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("picker: walk %s: %w", root, err)
		}
	} else {
		entries, err := os.ReadDir(root)
		if err != nil {
			return nil, fmt.Errorf("picker: read %s: %w", root, err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			p := filepath.Join(root, e.Name())
			if extSet[fileExt(p)] {
				out = append(out, p)
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

func fileExt(p string) string {
	return strings.TrimPrefix(strings.ToLower(filepath.Ext(p)), ".")
}
