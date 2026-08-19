// Package pathlink marks paths that name existing files and directories as terminal hyperlinks.
package pathlink

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	openPrefix = "\x1b]8;;"
	terminator = "\x1b\\"
	closeLink  = openPrefix + terminator
)

const pathExpression = `(?:~|\.{1,2})?/(?:[[:alnum:]_.@+%=-]+/)*[[:alnum:]_.@+%=-]+|[[:alnum:]_.@+%=-]+(?:/[[:alnum:]_.@+%=-]+)+|[[:alnum:]_@+%=-]+(?:\.[[:alnum:]_@+%=-]+)+|\.[[:alnum:]_@+%=-]+`

var pathPattern = regexp.MustCompile(`(` + pathExpression + `)(?::([0-9]+)(?::([0-9]+))?)?`)

// Render adds OSC 8 links to path-like spans which resolve beneath workspace or name an absolute
// path. ANSI styling around and within a path is preserved.
func Render(text string, workspace string) string {
	visible := visibleTextOf(text)
	matches := pathPattern.FindAllStringSubmatchIndex(visible.text, -1)
	if len(matches) == 0 {
		return text
	}

	var output strings.Builder
	sourceAt := 0

	for _, match := range matches {
		path := visible.text[match[2]:match[3]]
		target, exists := resolve(path, workspace)
		if !exists {
			continue
		}

		line := submatch(visible.text, match[4], match[5])
		column := submatch(visible.text, match[6], match[7])
		begin := visible.starts[match[0]]
		end := visible.ends[match[1]]
		output.WriteString(text[sourceAt:begin])
		output.WriteString(openPrefix)
		output.WriteString(linkURL(target, line, column))
		output.WriteString(terminator)
		output.WriteString(text[begin:end])
		output.WriteString(closeLink)
		sourceAt = end
	}

	if sourceAt == 0 {
		return text
	}

	output.WriteString(text[sourceAt:])
	return output.String()
}

type visibleText struct {
	text   string
	starts []int
	ends   []int
}

func visibleTextOf(text string) visibleText {
	var plain strings.Builder
	starts := []int{0}
	ends := []int{0}

	for index := 0; index < len(text); {
		if text[index] == '\x1b' {
			index = escapeEnd(text, index)
			starts[len(starts)-1] = index
			continue
		}

		plain.WriteByte(text[index])
		index++
		starts = append(starts, index)
		ends = append(ends, index)
	}

	return visibleText{text: plain.String(), starts: starts, ends: ends}
}

func escapeEnd(text string, start int) int {
	if start+1 >= len(text) {
		return len(text)
	}

	switch text[start+1] {
	case '[':
		for index := start + 2; index < len(text); index++ {
			if text[index] >= 0x40 && text[index] <= 0x7e {
				return index + 1
			}
		}
	case ']':
		for index := start + 2; index < len(text); index++ {
			switch {
			case text[index] == '\a':
				return index + 1
			case text[index] == '\x1b' && index+1 < len(text) && text[index+1] == '\\':
				return index + 2
			}
		}
	default:
		return min(start+2, len(text))
	}

	return len(text)
}

func submatch(text string, begin int, end int) string {
	if begin < 0 {
		return ""
	}

	return text[begin:end]
}

func resolve(path string, workspace string) (string, bool) {
	resolved := path

	if strings.HasPrefix(resolved, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}

		resolved = filepath.Join(home, strings.TrimPrefix(resolved, "~/"))
	} else if !filepath.IsAbs(resolved) {
		if workspace == "" {
			return "", false
		}

		resolved = filepath.Join(workspace, resolved)
	}

	resolved, err := filepath.Abs(resolved)
	if err != nil {
		return "", false
	}

	if _, err := os.Stat(resolved); err != nil {
		return "", false
	}

	return resolved, true
}

func linkURL(path string, line string, column string) string {
	address := &url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	if line != "" {
		address.Fragment = line
		if column != "" {
			address.Fragment += ":" + column
		}
	}

	return address.String()
}

func fileURL(path string) string {
	return linkURL(path, "", "")
}
