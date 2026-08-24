package terminal

import (
	"io"

	"crdx.org/io/cmd/oh/caps"
)

type terminalTitler interface {
	Begin(string) func()
	Set(string)
}

// Terminal holds the terminal facilities used by the harness.
type Terminal struct {
	title terminalTitler
}

// New builds the terminal facilities over writer.
func New(writer io.Writer) Terminal {
	return Terminal{
		title: newTitle(writer),
	}
}

// Begin starts the terminal facilities and returns their restore function.
func (self *Terminal) Begin(mode caps.Set) func() {
	if self.title == nil {
		return func() {}
	}

	return self.title.Begin(titleText(mode))
}

// SetMode updates terminal facilities that report the current capability mode.
func (self *Terminal) SetMode(mode caps.Set) {
	if self.title != nil {
		self.title.Set(titleText(mode))
	}
}

func titleText(mode caps.Set) string {
	if mode.Has(caps.Write) {
		return "[w]"
	} else {
		return "[r]"
	}
}
