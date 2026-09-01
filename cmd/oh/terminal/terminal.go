package terminal

import (
	"io"

	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/tty"
	"crdx.org/io/cmd/oh/work"
)

const writableMarker = "✱"

const eraseDisplay = "\x1b[H\x1b[2J\x1b[3J"

func ResetScrollback(writer io.Writer) {
	if tty.Is(writer) {
		_, _ = io.WriteString(writer, eraseDisplay)
	}
}

type terminalTitler interface {
	Begin(text string) func()
	Set(text string)
}

type Terminal struct {
	title         terminalTitler
	workspaceName string
	sessionTitle  string
	mode          caps.Set
	isBegun       bool
}

func New(writer io.Writer, workspace *work.Space) Terminal {
	return Terminal{
		title:         newTitle(writer),
		workspaceName: workspace.GetName(),
	}
}

func (self *Terminal) Begin(mode caps.Set) func() {
	self.mode = mode
	if self.title == nil {
		return func() {}
	}

	self.isBegun = true
	restore := self.title.Begin(self.titleText())

	return func() {
		self.isBegun = false
		restore()
	}
}

func (self *Terminal) SetMode(mode caps.Set) {
	self.mode = mode
	self.refreshTitle()
}

func (self *Terminal) SetSessionTitle(sessionTitle string) {
	if sessionTitle == self.sessionTitle {
		return
	}

	self.sessionTitle = sessionTitle
	self.refreshTitle()
}

func (self *Terminal) GetSessionTitle() string {
	return self.sessionTitle
}

func (self *Terminal) refreshTitle() {
	if self.title != nil && self.isBegun {
		self.title.Set(self.titleText())
	}
}

func (self *Terminal) titleText() string {
	subject := self.workspaceName
	if self.sessionTitle != "" {
		subject = self.sessionTitle
	}

	if self.mode.Has(caps.Write) {
		return subject + " " + writableMarker
	}

	return subject
}
