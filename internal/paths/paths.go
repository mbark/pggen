package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// WalkUp traverses up directory tree from dir until it finds an ancestor file
// named name. Checks the current directory first and then iteratively checks
// parent directories.
func WalkUp(dir, name string) (string, error) {
	for dir != string(os.PathSeparator) {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err != nil {
			if !os.IsNotExist(err) {
				return "", fmt.Errorf("stat file %s: %w", p, err)
			}
		} else {
			return dir, nil
		}
		dir = filepath.Dir(dir)
	}
	return "", fmt.Errorf("dir not found in directory tree starting from %s", dir)
}

// ExpandSortGlobs gets the absolute paths for all files matching globs. Order
// files lexicographically within each glob but not across all globs. The order
// of a glob relative to other globs is important for schemas where a schema
// might depend on a previous schema.
func ExpandSortGlobs(globs []string) ([]string, error) {
	files := make([]string, 0, len(globs)*4)
	for _, glob := range globs {
		var matches []string
		if !strings.ContainsAny(glob, "*?[{") {
			// A regular file, not a glob. Check if it exists.
			if _, err := os.Stat(glob); os.IsNotExist(err) {
				return nil, fmt.Errorf("file does not exist: %w", err)
			}
			matches = append(matches, glob)
		} else {
			base, pattern := doublestar.SplitPattern(filepath.ToSlash(glob))
			ms, err := doublestar.Glob(os.DirFS(base), pattern)
			if err != nil {
				// Ignore err, it's not helpful.
				return nil, fmt.Errorf("bad glob pattern: %s", glob)
			}
			for i, m := range ms {
				ms[i] = filepath.Join(base, m)
			}
			sort.Strings(ms)
			matches = ms
		}
		files = append(files, matches...)
	}
	for i, schema := range files {
		abs, err := filepath.Abs(schema)
		if err != nil {
			return nil, fmt.Errorf("absolute path for %s: %w", schema, err)
		}
		files[i] = abs
	}
	return files, nil
}
