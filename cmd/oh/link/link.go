package link

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"crdx.org/io/internal/util/pathutil"
)

const (
	openPrefix = "\x1b]8;;"
	terminator = "\x1b\\"
	closeLink  = openPrefix + terminator
)

const pathExpression = `(?:~|\.{1,2})?/(?:[[:alnum:]_.@+%=-]+/)*[[:alnum:]_.@+%=-]+|[[:alnum:]_.@+%=-]+(?:/[[:alnum:]_.@+%=-]+)+|[[:alnum:]_@+%=-]+(?:\.[[:alnum:]_@+%=-]+)+|\.[[:alnum:]_@+%=-]+`

var pathPattern = regexp.MustCompile(`(` + pathExpression + `)(?::([0-9]+)(?::([0-9]+))?)?`)

func RenderURL(text string, address string) string {
	return openPrefix + address + terminator + text + closeLink
}

func Plain(text string) string {
	return visibleTextOf(text).text
}

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

	for i := 0; i < len(text); {
		if text[i] == '\x1b' {
			i = escapeEnd(text, i)
			starts[len(starts)-1] = i
			continue
		}

		plain.WriteByte(text[i])
		i++
		starts = append(starts, i)
		ends = append(ends, i)
	}

	return visibleText{text: plain.String(), starts: starts, ends: ends}
}

func escapeEnd(text string, start int) int {
	if start+1 >= len(text) {
		return len(text)
	}

	switch text[start+1] {
	case '[':
		for i := start + 2; i < len(text); i++ {
			if text[i] >= 0x40 && text[i] <= 0x7e {
				return i + 1
			}
		}
	case ']':
		for i := start + 2; i < len(text); i++ {
			switch {
			case text[i] == '\a':
				return i + 1
			case text[i] == '\x1b' && i+1 < len(text) && text[i+1] == '\\':
				return i + 2
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
	resolvedPath, err := pathutil.Expand(path)
	if err != nil {
		return "", false
	}

	if !filepath.IsAbs(resolvedPath) {
		if workspace == "" {
			return "", false
		}

		resolvedPath = filepath.Join(workspace, resolvedPath)
	}

	resolvedPath, err = filepath.Abs(resolvedPath)
	if err != nil {
		return "", false
	}

	if _, err := os.Stat(resolvedPath); err != nil {
		return "", false
	}

	return resolvedPath, true
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
