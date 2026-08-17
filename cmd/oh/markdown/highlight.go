package markdown

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"

	"crdx.org/io/cmd/oh/theme"
)

// Highlight paints one line of source in the named language.
func Highlight(line string, language string) string {
	if language == "bash" {
		return bashCommand(line)
	}

	return highlight([]string{line}, language)[0]
}

func highlight(lines []string, language string) []string { // chroma reads it, the theme paints it
	lexer := lexers.Get(language)
	if language == "" || lexer == nil {
		return plainly(lines)
	}

	iterator, err := lexer.Tokenise(nil, strings.Join(lines, "\n"))
	if err != nil {
		return plainly(lines)
	}

	rows := []string{""}

	for _, token := range iterator.Tokens() {
		style := tokenStyle(token.Type)

		for index, piece := range strings.Split(token.Value, "\n") { // a style may not span a row
			if index > 0 {
				rows = append(rows, "")
			}

			switch {
			case piece == "":
			case strings.TrimSpace(piece) == "":
				rows[len(rows)-1] += piece // nothing to see in painted whitespace but the escapes
			default:
				rows[len(rows)-1] += style(piece)
			}
		}
	}

	return rows[:min(len(rows), len(lines))] // the lexer ends on a newline of its own
}

func bashCommand(line string) string {
	command, arguments, found := strings.Cut(line, " ")
	if command == "" {
		return theme.Block(line)
	}
	if !found {
		return theme.Function(command)
	}

	return theme.Function(command) + theme.Block(" "+arguments)
}

func plainly(lines []string) []string {
	highlightedLines := make([]string, len(lines))

	for index, line := range lines {
		highlightedLines[index] = theme.Block(line)
	}

	return highlightedLines
}

func tokenStyle(token chroma.TokenType) theme.Style {
	switch token.Category() {
	case chroma.Comment:
		return theme.Comment
	case chroma.Keyword:
		return keyword(token)
	case chroma.Literal:
		return literal(token)
	case chroma.Name:
		return name(token)
	case chroma.Operator:
		return theme.Operator
	case chroma.Punctuation:
		return theme.Punctuation
	case chroma.Text, chroma.Error, chroma.Other, chroma.Generic:
		return theme.Block
	}

	return theme.Block
}

func keyword(token chroma.TokenType) theme.Style {
	if token == chroma.KeywordType {
		return theme.Type
	}

	return theme.Keyword
}

func literal(token chroma.TokenType) theme.Style {
	if token.SubCategory() == chroma.LiteralNumber {
		return theme.Number
	}

	return theme.Literal
}

func name(token chroma.TokenType) theme.Style {
	switch token {
	case chroma.NameFunction, chroma.NameFunctionMagic:
		return theme.Function
	case chroma.NameClass, chroma.NameNamespace, chroma.NameBuiltin:
		return theme.Type
	}

	return theme.Variable
}
