package escape

import (
	"strconv"
	"strings"
)

const (
	textSizingPrefix   = "\x1b]66;"
	maximumCursorCells = 65_535
)

type Sequence struct {
	End     int
	Text    string
	Cells   int
	IsStyle bool
}

func GetSequence(runes []rune, start int) Sequence {
	if start+1 >= len(runes) {
		return Sequence{End: len(runes)}
	}

	switch runes[start+1] {
	case '[':
		end := getCSIEnd(runes, start)
		if cells, isCursorRight := getCursorRight(string(runes[start:end])); isCursorRight {
			return Sequence{End: end, Cells: cells}
		}
		return Sequence{End: end, IsStyle: true}
	case ']':
		end, isTerminated := getOSCEnd(runes, start)
		if !isTerminated {
			return Sequence{End: end}
		}
		text, cells := getTextSizing(string(runes[start:end]))
		return Sequence{End: end, Text: text, Cells: cells}
	default:
		return Sequence{End: getLegacyEnd(runes, start)}
	}
}

func GetEnd(runes []rune, start int) int {
	return GetSequence(runes, start).End
}

func getCSIEnd(runes []rune, start int) int {
	for end := start + 2; end < len(runes); end++ {
		if runes[end] >= 0x40 && runes[end] <= 0x7e {
			return end + 1
		}
	}
	return len(runes)
}

func getCursorRight(sequence string) (int, bool) {
	if !strings.HasPrefix(sequence, "\x1b[") || !strings.HasSuffix(sequence, "C") {
		return 0, false
	}

	cells, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(sequence, "\x1b["), "C"))
	if err != nil || cells <= 0 {
		return 0, false
	}
	return min(cells, maximumCursorCells), true
}

func getOSCEnd(runes []rune, start int) (int, bool) {
	for end := start + 2; end < len(runes); end++ {
		switch {
		case runes[end] == '\a':
			return end + 1, true
		case runes[end] == '\x1b' && end+1 < len(runes) && runes[end+1] == '\\':
			return end + 2, true
		}
	}
	return len(runes), false
}

func getLegacyEnd(runes []rune, start int) int {
	end := start + 1
	for end < len(runes) && runes[end] != 'm' && runes[end] != 'K' {
		end++
	}
	if end < len(runes) {
		end++
	}
	return end
}

func getTextSizing(sequence string) (string, int) {
	if !strings.HasPrefix(sequence, textSizingPrefix) {
		return "", 0
	}

	body := strings.TrimPrefix(sequence, textSizingPrefix)
	body = strings.TrimSuffix(strings.TrimSuffix(body, "\x1b\\"), "\a")
	metadata, text, found := strings.Cut(body, ";")
	if !found {
		return "", 0
	}

	scale, scaledWidth := 1, 0
	hasScaledWidth := false
	for field := range strings.SplitSeq(metadata, ":") {
		key, value, found := strings.Cut(field, "=")
		if !found {
			continue
		}
		parsed, err := strconv.Atoi(value)
		if err != nil {
			if key == "s" || key == "w" {
				return "", 0
			}
			continue
		}
		switch key {
		case "s":
			if parsed < 1 || parsed > 7 {
				return "", 0
			}
			scale = parsed
		case "w":
			if parsed < 1 || parsed > 7 {
				return "", 0
			}
			scaledWidth = parsed
			hasScaledWidth = true
		}
	}
	if !hasScaledWidth || text == "" {
		return "", 0
	}
	return text, scale * scaledWidth
}
