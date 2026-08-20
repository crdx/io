package line

import (
	"os"
	"path/filepath"
	"strings"
)

// History is the entries typed before, oldest first, one per line of the file with newlines
// escaped.
type History struct {
	path  string
	limit int
	lines []string // the entries, oldest first
}

// NewHistory reads the history file. A missing one is what a first run looks like.
func NewHistory(path string, limit int) *History {
	self := &History{path: path, limit: limit}

	data, err := os.ReadFile(path) //nolint:gosec // the path is ours, not user input
	if err != nil {
		return self
	}

	for line := range strings.SplitSeq(string(data), "\n") {
		if line != "" {
			self.lines = append(self.lines, unescape(line))
		}
	}

	self.trim()

	return self
}

// Add records an entry, unless it is blank or the one just entered.
func (self *History) Add(line string) {
	if strings.TrimSpace(line) == "" {
		return
	}

	if len(self.lines) > 0 && self.lines[len(self.lines)-1] == line {
		return
	}

	self.lines = append(self.lines, line)
	self.trim()
	self.save()
}

func (self *History) recall() *recall {
	return &recall{lines: self.lines, index: len(self.lines)}
}

func (self *History) trim() {
	if self.limit > 0 && len(self.lines) > self.limit {
		self.lines = self.lines[len(self.lines)-self.limit:]
	}
}

func (self *History) save() {
	if self.path == "" {
		return
	}

	if err := os.MkdirAll(filepath.Dir(self.path), 0o700); err != nil {
		return
	}

	var out strings.Builder

	for _, line := range self.lines {
		out.WriteString(escape(line))
		out.WriteString("\n")
	}

	_ = os.WriteFile(self.path, []byte(out.String()), 0o600)
}

func escape(line string) string {
	return strings.ReplaceAll(strings.ReplaceAll(line, `\`, `\\`), "\n", `\n`)
}

func unescape(line string) string {
	var out strings.Builder

	escaped := false

	for _, value := range line {
		switch {
		case escaped && value == 'n':
			out.WriteRune('\n')
			escaped = false
		case escaped:
			if value != '\\' {
				out.WriteRune('\\')
			}
			out.WriteRune(value)
			escaped = false
		case value == '\\':
			escaped = true
		default:
			out.WriteRune(value)
		}
	}

	if escaped {
		out.WriteRune('\\')
	}

	return out.String()
}

type recall struct {
	lines        []string
	index        int
	pendingInput string
}

func (self *recall) Walk(current string, direction int) (string, bool) {
	if direction < 0 {
		if self.index == 0 {
			return "", false
		}

		if self.index == len(self.lines) {
			self.pendingInput = current
		}

		self.index--

		return self.lines[self.index], true
	}

	if self.index >= len(self.lines) {
		return "", false
	}

	self.index++

	if self.index == len(self.lines) {
		return self.pendingInput, true
	}

	return self.lines[self.index], true
}
