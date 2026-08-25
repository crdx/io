package terminal

import (
	"io"
	"path/filepath"

	"crdx.org/io/cmd/oh/caps"
)

const (
	readOnlyMarker = "⚫"
	writableMarker = "🟡"
)

type terminalTitler interface {
	Begin(string) func()
	Set(string)
}

// Terminal holds the terminal facilities used by the harness.
type Terminal struct {
	title         terminalTitler
	workspaceName string
}

// New builds the terminal facilities over writer.
func New(writer io.Writer, workspaceDir string) Terminal {
	return Terminal{
		title:         newTitle(writer),
		workspaceName: filepath.Base(workspaceDir),
	}
}

// Begin starts the terminal facilities and returns their restore function.
func (self *Terminal) Begin(mode caps.Set) func() {
	if self.title == nil {
		return func() {}
	}

	return self.title.Begin(titleText(mode, self.workspaceName))
}

// SetMode updates terminal facilities that report the current capability mode.
func (self *Terminal) SetMode(mode caps.Set) {
	if self.title != nil {
		self.title.Set(titleText(mode, self.workspaceName))
	}
}

func titleText(mode caps.Set, workspaceName string) string {
	if mode.Has(caps.Write) {
		return writableMarker + " " + workspaceName
	} else {
		return readOnlyMarker + " " + workspaceName
	}
}
