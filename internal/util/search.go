package util

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"crdx.org/io/internal/pathutil"
	"crdx.org/io/tool"
)

// MaxSearchBytes caps the matching content.
const MaxSearchBytes = 16 * 1024

var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
}

// RenderSearch describes a search.
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

// Walk visits every entry below root within filesystem, skipping symlinks and the unwanted dirs.
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

// AppendSearchResult adds a complete result where it fits within the shared search byte cap.
func AppendSearchResult(results []string, returnedBytes int64, result string) ([]string, int64, bool) {
	separatorBytes := 0
	if len(results) > 0 {
		separatorBytes = 1
	}
	resultBytes := int64(separatorBytes + len(result))
	if returnedBytes+resultBytes > MaxSearchBytes {
		return results, returnedBytes, true
	}

	return append(results, result), returnedBytes + resultBytes, false
}

// ReportSearchResults renders what a search found, saying so where the byte cap was hit.
func ReportSearchResults(results []string, truncated bool) string {
	joinedResults := strings.Join(results, "\n")
	if truncated {
		if joinedResults != "" {
			joinedResults += "\n\n"
		}

		return fmt.Sprintf(
			"%s[stopped before matching output exceeded %s; narrow the search to see the rest]",
			joinedResults, FormatBytes(MaxSearchBytes, 3),
		)
	}
	if joinedResults == "" {
		return "(no matches)"
	}

	return joinedResults
}
