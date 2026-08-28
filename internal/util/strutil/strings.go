package strutil

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

func Capitalise(text string) string {
	characters := []rune(text)
	if len(characters) > 0 {
		characters[0] = unicode.ToUpper(characters[0])
	}
	return string(characters)
}

func FirstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(line)
}

func Flatten(text string) string {
	return strings.Join(strings.Fields(stripped(text)), " ")
}

func Printable(text string) string {
	var out strings.Builder

	out.Grow(len(text))

	for _, character := range text {
		if !unicode.IsSpace(character) && !unicode.IsControl(character) {
			out.WriteRune(character)
			continue
		}

		for range utf8.RuneLen(character) {
			out.WriteByte(' ')
		}
	}

	return out.String()
}

const (
	controlSequence = `\[[0-?]*[ -/]*[@-~]?`
	commandString   = `\][^\a\x1b]*(?:\a|\x1b\\)?`
	otherSequence   = `[ -/]*[0-~]?`
)

var escapeSequence = regexp.MustCompile(
	`\x1b(?:` + controlSequence + `|` + commandString + `|` + otherSequence + `)`,
)

func stripped(text string) string {
	return strings.Map(strippedRune, escapeSequence.ReplaceAllString(text, ""))
}

func strippedRune(character rune) rune {
	switch {
	case unicode.IsSpace(character):
		return ' '
	case unicode.IsControl(character):
		return -1
	default:
		return character
	}
}

func Lines(text string) []string {
	if text == "" {
		return nil
	}

	lines := strings.Split(text, "\n")
	if strings.HasSuffix(text, "\n") {
		lines = lines[:len(lines)-1]
	}

	return lines
}
