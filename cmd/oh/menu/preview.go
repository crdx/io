package menu

import (
	"fmt"
	"strings"

	"crdx.org/io/cmd/oh/ansi"
	"crdx.org/io/cmd/oh/key"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/width"
)

const (
	openablePreviewHint = "enter to open · esc return · ↑↓ scroll"
	runningPreviewHint  = "running · esc return · ↑↓ scroll"
	closePreviewKey     = 'q'
	previewRule         = "─"
	previewHeaderRows   = 3
)

type previewState struct {
	title        string
	read         func(room int) ([]string, error)
	rows         []string
	room         int
	offset       int
	failure      string
	isOpen       bool
	isOpenable   bool
	isTailWanted bool
}

func (self *state) askToPreview(keypress key.Key) bool {
	rows, isPreviewable := self.list.(Previewable)
	if !isPreviewable || self.cursor < 0 {
		return false
	}

	work, isBound := rows.Preview(self.chosen(), keypress)
	if !isBound {
		return false
	}

	self.preview = previewState{
		title:        work.Title,
		read:         work.Read,
		isOpen:       true,
		isOpenable:   self.list.IsChoosable(self.chosen()),
		isTailWanted: true,
	}

	return true
}

func (self *state) readPreview(keypress key.Key) action {
	window := max(self.window, 1)

	switch keypress.Code {
	case key.Up:
		self.scrollPreview(-1)
	case key.Down:
		self.scrollPreview(1)
	case key.PageUp:
		self.scrollPreview(-window)
	case key.PageDown:
		self.scrollPreview(window)
	case key.Home:
		self.preview.offset = 0
	case key.End:
		self.preview.offset = self.lastPreviewOffset()
	case key.Enter:
		if self.cursor >= 0 && self.list.IsChoosable(self.chosen()) {
			return rowChosen
		}
	case key.Escape:
		self.preview = previewState{}
	case key.Rune:
		if keypress.Value == 'c' && keypress.Mod.Has(key.Ctrl) {
			return choiceCancelled
		}
		if keypress.Value == closePreviewKey && !keypress.Mod.Has(key.Ctrl) {
			self.preview = previewState{}
		}
	case key.Left, key.Right, key.Backspace, key.Delete, key.PasteStart, key.PasteEnd,
		key.Clipboard, key.FocusIn, key.FocusOut, key.Unknown:
	}

	return continuePicking
}

func (self *state) scrollPreview(distance int) {
	self.preview.offset = min(max(self.preview.offset+distance, 0), self.lastPreviewOffset())
}

func (self *state) lastPreviewOffset() int {
	return max(len(self.preview.rows)-max(self.window, 1), 0)
}

func (self *state) readPreviewRows(room int) {
	if self.preview.read == nil || self.preview.room == room {
		return
	}

	self.preview.room = room
	self.preview.failure = ""

	rows, err := self.preview.read(room)
	if err != nil {
		self.preview.rows = nil
		self.preview.failure = err.Error()
		return
	}

	self.preview.rows = rows
	if self.preview.isTailWanted {
		self.preview.isTailWanted = false
		self.preview.offset = self.lastPreviewOffset()
		return
	}

	self.preview.offset = min(self.preview.offset, self.lastPreviewOffset())
}

func (self *state) drawPreview(room int, height int) string {
	rows := max(height-previewHeaderRows, 1)
	self.window = rows
	self.readPreviewRows(room)
	self.preview.offset = min(self.preview.offset, self.lastPreviewOffset())

	var output strings.Builder

	output.WriteString(homeCursor)
	output.WriteString(self.previewTitle(room))
	output.WriteString(eraseLine + "\r\n")
	output.WriteString(self.previewHint(room))
	output.WriteString(eraseLine + "\r\n")
	output.WriteString(style.Rule(strings.Repeat(previewRule, max(room, 0))))
	output.WriteString(eraseLine + "\r\n")

	for at := self.preview.offset; at < self.preview.offset+rows; at++ {
		if at < len(self.preview.rows) && self.preview.rows[at] != "" {
			output.WriteString(Clip(self.preview.rows[at], room))
			output.WriteString(ansi.Reset)
		}
		output.WriteString(eraseLine)
		if at+1 < self.preview.offset+rows {
			output.WriteString("\r\n")
		}
	}

	output.WriteString(eraseBelow)

	return output.String()
}

func (self *state) previewHint(room int) string {
	if self.preview.isOpenable {
		return style.Subtle(Clip(openablePreviewHint, room))
	}

	mark, rest, isMarked := strings.Cut(Clip(runningPreviewHint, room), " ")
	if !isMarked {
		return style.Success(mark)
	}

	return style.Success(mark) + style.Subtle(" "+rest)
}

func (self *state) previewTitle(room int) string {
	if self.preview.failure != "" {
		return style.Failure(Clip(self.preview.failure, room))
	}

	position := self.previewPosition()
	gap := room - width.Of(self.preview.title) - width.Of(position)
	if position == "" || gap < minimumGap {
		return style.Prompt(Clip(self.preview.title, room))
	}

	return style.Prompt(self.preview.title) + strings.Repeat(" ", gap) + style.Subtle(position)
}

func (self *state) previewPosition() string {
	total := len(self.preview.rows)
	if total == 0 {
		return ""
	}

	last := min(self.preview.offset+max(self.window, 1), total)

	return fmt.Sprintf("%d-%d of %d", self.preview.offset+1, last, total)
}
