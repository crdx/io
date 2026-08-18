package util

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"crdx.org/io/internal/pathutil"
	"crdx.org/io/tool"
)

// MaxMatches caps what a search hands back.
const MaxMatches = 100

var skipDirs = map[string]bool{".git": true, "node_modules": true}

// RenderSearch describes a search out loud, as the pattern and what qualifies it. A search taking
// no glob passes an empty string, and the working directory goes without saying.
func RenderSearch(pattern string, path string, globPattern string) (string, string) {
	var detail string

	if path != "" && path != "." {
		detail += "in " + pathutil.Shorten(path)
	}

	if globPattern != "" {
		if detail != "" {
			detail += " "
		}

		detail += "(" + globPattern + ")"
	}

	return pattern, detail
}

// SearchPath returns the final component of the path in a rendered search call.
func SearchPath(call tool.Call) string {
	detail := strings.TrimPrefix(call.Detail(), "in ")
	if detail == call.Detail() {
		return ""
	}

	path, _, _ := strings.Cut(detail, " (")
	return filepath.Base(path)
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
