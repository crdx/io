package input

import (
	"strings"

	"crdx.org/io/cmd/oh/edit"
	"crdx.org/io/cmd/oh/style"
)

const (
	startPad = 1
	endPad   = 2
)

type Ruler struct {
	Left  string
	Right string
}

type Block struct {
	Top    Ruler
	Input  edit.Frame
	Bottom Ruler
}

func (self Block) Rows(width int) ([]string, int, int) {
	rows := make([]string, 0, len(self.Input.Rows)+3)

	bottom := self.Bottom
	if getWidth(bottom.Left, startPad)+getWidth(bottom.Right, endPad) > width {
		bottom.Right = ""
	}

	rows = append(rows, self.Top.render(width))
	rows = append(rows, self.Input.Rows...)
	rows = append(rows, bottom.render(width))

	return rows, self.Input.Row + 1, self.Input.Column
}

func (self Ruler) render(width int) string {
	head := ""
	n := getWidth(self.Left, startPad)

	if n > 0 && n+getWidth(self.Right, endPad) <= width {
		head = style.Rule(strings.Repeat("─", startPad)) + " " + self.Left + " "
		width -= n
	}

	return head + ruleTo(width, self.Right)
}

func ruleTo(width int, label string) string {
	n := getWidth(label, endPad)
	if n == 0 || n > width {
		return style.Rule(strings.Repeat("─", max(width, 0)))
	}

	a := style.Rule(strings.Repeat("─", width-n))
	b := style.Rule(strings.Repeat("─", endPad))

	return a + " " + label + " " + b
}

func getWidth(str string, edgePadding int) int {
	cells := style.Width(str)
	if cells == 0 {
		return 0
	}

	return cells + edgePadding + 2
}
