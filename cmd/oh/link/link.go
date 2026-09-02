package link

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"crdx.org/io/cmd/oh/escape"
	"crdx.org/io/internal/util/pathutil"
)

const (
	openPrefix = "\x1b]8;;"
	terminator = "\x1b\\"
	closeLink  = escape.HyperlinkClose
)

const pathExpression = `(?:~|\.{1,2})?/(?:[[:alnum:]_.@+%=-]+/)*[[:alnum:]_.@+%=-]+|[[:alnum:]_.@+%=-]+(?:/[[:alnum:]_.@+%=-]+)+|[[:alnum:]_@+%=-]+(?:\.[[:alnum:]_@+%=-]+)+|\.[[:alnum:]_@+%=-]+`

var pathPattern = regexp.MustCompile(`(` + pathExpression + `)(?::([0-9]+)(?::([0-9]+))?)?`)

func RenderURL(text string, address string) string {
	return openPrefix + address + terminator + text + closeLink
}

func RenderWebURL(text string, address string) string {
	target, isSupported := webURL(address)
	if !isSupported {
		return text
	}

	return RenderURL(text, target)
}

func webURL(address string) (string, bool) {
	target, err := url.Parse(address)
	if err != nil {
		return "", false
	}

	switch strings.ToLower(target.Scheme) {
	case "http", "https":
		if target.Host == "" {
			return "", false
		}
	case "mailto":
		if target.Opaque == "" && target.Path == "" {
			return "", false
		}
	default:
		return "", false
	}

	return target.String(), true
}

func Plain(text string) string {
	return visibleTextOf(text).text
}

func Render(text string, workspace string) string {
	visible := visibleTextOf(text)
	matches := pathPattern.FindAllStringSubmatchIndex(visible.text, -1)
	if len(matches) == 0 {
		return text
	}

	var output strings.Builder
	sourceAt := 0

	for _, match := range matches {
		if visible.hasLink(match[0], match[1]) {
			continue
		}

		pathAt, target, exists := locate(visible.text[match[2]:match[3]], workspace)
		if !exists {
			continue
		}

		line := submatch(visible.text, match[4], match[5])
		column := submatch(visible.text, match[6], match[7])
		begin := visible.starts[match[2]+pathAt]
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
	text     string
	starts   []int
	ends     []int
	isLinked []bool
}

func (self visibleText) hasLink(begin int, end int) bool {
	for _, isLinked := range self.isLinked[begin:end] {
		if isLinked {
			return true
		}
	}

	return false
}

func visibleTextOf(text string) visibleText {
	var plain strings.Builder
	starts := []int{0}
	ends := []int{0}
	var isLinked []bool
	isLinkActive := false

	for i := 0; i < len(text); {
		if text[i] == '\x1b' {
			end := escapeEnd(text, i)
			sequence := text[i:end]
			if strings.HasPrefix(sequence, openPrefix) {
				isLinkActive = sequence != closeLink
			}
			i = end
			starts[len(starts)-1] = i
			continue
		}

		plain.WriteByte(text[i])
		isLinked = append(isLinked, isLinkActive)
		i++
		starts = append(starts, i)
		ends = append(ends, i)
	}

	return visibleText{text: plain.String(), starts: starts, ends: ends, isLinked: isLinked}
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

func locate(candidate string, workspace string) (int, string, bool) {
	if target, exists := resolve(candidate, workspace); exists {
		return 0, target, true
	}

	assignedAt := strings.LastIndexByte(candidate, '=') + 1
	if assignedAt > 0 && assignedAt < len(candidate) {
		if target, exists := resolve(candidate[assignedAt:], workspace); exists {
			return assignedAt, target, true
		}
	}

	return 0, "", false
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
