package util

import (
	"fmt"
	"io/fs"
	"strings"
)

// MaxMatches caps what a search hands back.
const MaxMatches = 100

var skipDirs = map[string]bool{".git": true, "node_modules": true}

// RenderSearch describes a search out loud. A search taking no glob passes an empty string, and the
// working directory goes without saying.
func RenderSearch(pattern string, path string, globPattern string) string {
	str := pattern

	if path != "" && path != "." {
		str += " in " + path
	}

	if globPattern != "" {
		str += " matching " + globPattern
	}

	return str
}

// Walk visits every entry below root within filesystem, skipping symlinks and the directories
// nothing wants searched.
func Walk(filesystem fs.FS, root string, visit func(path string, entry fs.DirEntry) error) error {
	return fs.WalkDir(filesystem, root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}

			return nil
		}

		if entry.IsDir() && skipDirs[entry.Name()] && path != root {
			return fs.SkipDir
		}

		if entry.Type()&fs.ModeSymlink != 0 && path != root {
			return nil
		}

		return visit(path, entry)
	})
}

// Report renders what a search found, saying so where the cap was hit.
func Report(matches []string, truncated bool) string {
	if len(matches) == 0 {
		return "(no matches)"
	}

	if truncated {
		return fmt.Sprintf(
			"%s\n\n[stopped at %d matches, narrow the search to see the rest]",
			strings.Join(matches, "\n"), MaxMatches,
		)
	}

	return strings.Join(matches, "\n")
}
