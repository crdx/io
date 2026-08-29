package markdown

import (
	"strconv"
	"strings"

	"crdx.org/col"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"

	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/width"
	"crdx.org/io/internal/mermaid"
)

const tab = "    "

const mermaidLanguage = "mermaid"

var parser = goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser()

// Render lays markdown out as the rows to draw, styled and wrapped to the given columns.
func Render(markdown string, columns int) []string {
	return render(markdown, columns, nil)
}

// StreamRenderer retains successful Mermaid diagrams while incomplete Markdown continues arriving.
type StreamRenderer struct {
	mermaidRows   map[int][]string
	isTailMermaid bool
}

// Render lays out the current prefix of a Markdown stream.
func (self *StreamRenderer) Render(markdown string, columns int) []string {
	self.isTailMermaid = false

	return render(markdown, columns, self)
}

// IsTailMermaid reports whether the last rendered row belongs to a Mermaid diagram.
func (self *StreamRenderer) IsTailMermaid() bool {
	return self.isTailMermaid
}

// Reset forgets rendering state from the previous stream.
func (self *StreamRenderer) Reset() {
	clear(self.mermaidRows)
	self.isTailMermaid = false
}

func render(markdown string, columns int, stream *StreamRenderer) []string {
	source := []byte(strings.ReplaceAll(markdown, "\t", tab))

	mermaidBlock := 0
	renderer := &renderer{source: source, columns: columns, mermaidBlock: &mermaidBlock, stream: stream}
	renderer.blocks(parser.Parse(text.NewReader(source)))

	return renderer.rows
}

type renderer struct {
	source       []byte // the markdown being drawn, which every node is a position in
	columns      int
	mermaidBlock *int
	isTight      bool     // whether its blocks stand apart, which those of a tight list do not
	rows         []string // what has been drawn so far
	stream       *StreamRenderer
}

func (self *renderer) blocks(parent ast.Node) {
	for node := parent.FirstChild(); node != nil; node = node.NextSibling() {
		if len(self.rows) > 0 && !self.isTight {
			self.rows = append(self.rows, "")
		}

		self.block(node)
	}

	for len(self.rows) > 0 && self.rows[len(self.rows)-1] == "" {
		self.rows = self.rows[:len(self.rows)-1]
	}
}

func (self *renderer) block(node ast.Node) {
	if self.stream != nil {
		self.stream.isTailMermaid = false
	}

	switch node := node.(type) {
	case *ast.Heading:
		self.appendWrapped(over(col.Bold, style.Heading(self.inline(node))))

	case *ast.FencedCodeBlock:
		language := string(node.Language(self.source))
		lines := self.lines(node)
		if language == mermaidLanguage {
			block := *self.mermaidBlock
			*self.mermaidBlock++
			if self.mermaid(lines, block) {
				if self.stream != nil {
					self.stream.isTailMermaid = true
				}

				return
			}
		}
		self.code(emphasise(lines, language))

	case *ast.CodeBlock:
		self.code(emphasise(self.lines(node), ""))

	case *ast.HTMLBlock:
		self.code(emphasise(self.lines(node), ""))

	case *ast.ThematicBreak:
		self.rows = append(self.rows, style.Border(strings.Repeat("─", max(self.columns, 0))))

	case *ast.Blockquote:
		self.quote(node)

	case *ast.List:
		self.list(node)

	case *east.Table:
		self.rows = append(self.rows, self.table(node)...)

	default:
		self.appendWrapped(self.inline(node))
	}
}

func (self *renderer) appendWrapped(styled string) {
	self.rows = append(self.rows, width.Wrap(styled, self.columns)...)
}

func (self *renderer) code(lines []string) {
	for _, line := range lines {
		if strings.TrimSpace(style.Plain(line)) == "" {
			self.rows = append(self.rows, "")
			continue
		}

		self.rows = append(self.rows, width.Wrap(line, self.columns)...)
	}
}

func (self *renderer) mermaid(lines []string, block int) bool {
	if rows, isDrawable := renderMermaidRows(lines, self.columns); isDrawable {
		self.rows = append(self.rows, rows...)
		self.rememberMermaidRows(block, rows)
		return true
	}

	if self.stream == nil {
		return false
	}
	cachedRows, hasCachedRows := self.stream.mermaidRows[block]
	if !hasCachedRows || !rowsFit(cachedRows, self.columns) {
		return false
	}
	self.rows = append(self.rows, cachedRows...)
	return true
}

func renderMermaidRows(lines []string, columns int) ([]string, bool) {
	diagram, err := mermaid.Render(strings.Join(lines, "\n"))
	if err != nil || diagram == "" {
		return nil, false
	}

	rows := strings.Split(diagram, "\n")
	if !rowsFit(rows, columns) {
		return nil, false
	}

	return rows, true
}

func (self *renderer) rememberMermaidRows(block int, rows []string) {
	if self.stream == nil {
		return
	}

	if self.stream.mermaidRows == nil {
		self.stream.mermaidRows = map[int][]string{}
	}

	self.stream.mermaidRows[block] = rows
}

func rowsFit(rows []string, columns int) bool {
	for _, row := range rows {
		if width.Of(row) > columns {
			return false
		}
	}
	return true
}

func (self *renderer) quote(node ast.Node) {
	lead, room := margin(self.columns, "│ ")

	inner := &renderer{source: self.source, columns: room, mermaidBlock: self.mermaidBlock, stream: self.stream}
	inner.blocks(node)

	for _, row := range inner.rows {
		self.rows = append(self.rows, style.Border(lead)+over(style.Quote, row))
	}
}

func (self *renderer) list(node *ast.List) {
	number := node.Start

	for item := node.FirstChild(); item != nil; item = item.NextSibling() {
		marker := "• "

		if node.IsOrdered() {
			marker = strconv.Itoa(number) + ". "
			number++
		}

		self.item(marker, item)
	}
}

func (self *renderer) item(marker string, node ast.Node) {
	if self.stream != nil {
		self.stream.isTailMermaid = false
	}

	room := self.columns - width.Of(marker)
	if room < 1 {
		self.appendWrapped(style.Bullet(marker) + self.inline(node))
		return
	}

	inner := &renderer{source: self.source, columns: room, mermaidBlock: self.mermaidBlock, isTight: true, stream: self.stream}
	inner.blocks(node)

	hangingIndent := strings.Repeat(" ", width.Of(marker))

	for i, row := range inner.rows {
		if i == 0 {
			self.rows = append(self.rows, style.Bullet(marker)+row)
			continue
		}

		self.rows = append(self.rows, hangingIndent+row)
	}
}

func (self *renderer) lines(node ast.Node) []string {
	segments := node.Lines()
	lines := make([]string, 0, segments.Len())

	for i := range segments.Len() {
		segment := segments.At(i)
		lines = append(lines, strings.TrimRight(string(segment.Value(self.source)), "\n"))
	}

	return lines
}

func margin(cells int, prefix string) (string, int) {
	if prefixWidth := width.Of(prefix); cells > prefixWidth {
		return prefix, cells - prefixWidth
	}

	return "", cells
}
