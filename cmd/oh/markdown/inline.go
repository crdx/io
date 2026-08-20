package markdown

import (
	"strings"

	"crdx.org/col"

	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/util"

	"crdx.org/io/cmd/oh/style"
)

const reset = "\x1b[0m"

func (self *renderer) inline(parent ast.Node) string {
	var out strings.Builder

	for node := parent.FirstChild(); node != nil; node = node.NextSibling() {
		out.WriteString(self.renderInlineNode(node))
	}

	return out.String()
}

func (self *renderer) renderInlineNode(node ast.Node) string {
	switch node := node.(type) {
	case *ast.Text:
		return style.Answer(self.text(node)) + lineBreak(node)

	case *ast.String:
		return style.Answer(string(node.Value))

	case *ast.CodeSpan:
		return style.Code(self.words(node))

	case *ast.Emphasis:
		if node.Level >= 2 {
			return over(col.Bold, self.inline(node))
		}

		return over(col.Italic, self.inline(node))

	case *east.Strikethrough:
		return over(col.Strikethrough, self.inline(node))

	case *ast.Link:
		return style.Link(self.inline(node)) + style.Address(" ("+string(node.Destination)+")")

	case *ast.Image:
		return style.Link(self.inline(node)) + style.Address(" ("+string(node.Destination)+")")

	case *ast.AutoLink:
		return style.Link(string(node.URL(self.source)))

	case *ast.RawHTML:
		return style.Subtle(self.raw(node))

	case *east.TaskCheckBox:
		if node.IsChecked {
			return style.Success("[x] ")
		}

		return style.Subtle("[ ] ")
	}

	return self.inline(node)
}

func (self *renderer) text(node *ast.Text) string { // markdown puts escapes in, so they come out
	value := node.Segment.Value(self.source)

	if node.IsRaw() {
		return string(value)
	}

	return string(util.ResolveEntityNames(util.ResolveNumericReferences(
		util.UnescapePunctuations(value),
	)))
}

func lineBreak(node *ast.Text) string { // a line the model chose to break is a line it meant to break
	if node.SoftLineBreak() || node.HardLineBreak() {
		return "\n"
	}

	return ""
}

func (self *renderer) words(parent ast.Node) string {
	var out strings.Builder

	for node := parent.FirstChild(); node != nil; node = node.NextSibling() {
		if textNode, is := node.(*ast.Text); is {
			out.Write(textNode.Segment.Value(self.source))
		}
	}

	return out.String()
}

func (self *renderer) raw(node *ast.RawHTML) string {
	var out strings.Builder

	for index := range node.Segments.Len() {
		segment := node.Segments.At(index)
		out.Write(segment.Value(self.source))
	}

	return out.String()
}

func over(paint style.Style, text string) string { // a style inside another is off from its reset on
	stylePrefix := strings.TrimSuffix(paint(""), reset)
	if stylePrefix == "" {
		return text
	}

	return paint(strings.TrimSuffix(strings.ReplaceAll(text, reset, reset+stylePrefix), stylePrefix))
}
