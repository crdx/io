package terminal

import (
	"io"
	"path/filepath"

	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/tty"
)

const writableMarker = "✱"

const eraseDisplay = "\x1b[H\x1b[2J\x1b[3J"

func ResetScrollback(writer io.Writer) {
	if tty.Is(writer) {
		_, _ = io.WriteString(writer, eraseDisplay)
	}
}

type terminalTitler interface {
	Begin(string) func()
	Set(string)
}

type Terminal struct {
	title         terminalTitler
	workspaceName string
}

func New(writer io.Writer, workspaceDir string) Terminal {
	return Terminal{
		title:         newTitle(writer),
		workspaceName: filepath.Base(workspaceDir),
	}
}

func (self *Terminal) Begin(mode caps.Set) func() {
	if self.title == nil {
		return func() {}
	}

	return self.title.Begin(titleText(mode, self.workspaceName))
}

func (self *Terminal) SetMode(mode caps.Set) {
	if self.title != nil {
		self.title.Set(titleText(mode, self.workspaceName))
	}
}

func titleText(mode caps.Set, workspaceName string) string {
	if mode.Has(caps.Write) {
		return workspaceName + " " + writableMarker
	} else {
		return workspaceName
	}
}
