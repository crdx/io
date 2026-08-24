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
	Left   string
	Center string
	Right  string
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
	leftWidth := getWidth(self.Left, startPad)
	rightWidth := getWidth(self.Right, endPad)

	head := ""
	if leftWidth == 0 || leftWidth+rightWidth > width {
		leftWidth = 0
	} else {
		head = style.Rule(strings.Repeat("─", startPad)) + " " + self.Left + " "
	}

	tail := ""
	if rightWidth == 0 || leftWidth+rightWidth > width {
		rightWidth = 0
	} else {
		tail = " " + self.Right + " " + style.Rule(strings.Repeat("─", endPad))
	}

	middleWidth := max(width-leftWidth-rightWidth, 0)

	return head + renderCentredSpan(middleWidth, self.Center, leftWidth, width) + tail
}

func renderCentredSpan(availableWidth int, center string, startColumn int, ruleWidth int) string {
	centerWidth := getWidth(center, 0)
	beforeWidth := (ruleWidth-centerWidth)/2 - startColumn
	if centerWidth == 0 || beforeWidth < 0 || beforeWidth+centerWidth > availableWidth {
		return style.Rule(strings.Repeat("─", availableWidth))
	}

	before := style.Rule(strings.Repeat("─", beforeWidth))
	after := style.Rule(strings.Repeat("─", availableWidth-centerWidth-beforeWidth))

	return before + " " + center + " " + after
}

func getWidth(str string, edgePadding int) int {
	cells := style.Width(str)
	if cells == 0 {
		return 0
	}

	return cells + edgePadding + 2
}
