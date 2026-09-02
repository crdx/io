package tty

import (
	"errors"
	"io"
	"os"

	"golang.org/x/term"

	"crdx.org/io/cmd/oh/key"
)

var ErrNotTerminal = errors.New("not a terminal")

func Is(stream any) bool {
	file, ok := stream.(*os.File)

	return ok && term.IsTerminal(int(file.Fd()))
}

const controllingTerminal = "/dev/tty"

func Keyboard(input *os.File) (*os.File, func()) {
	if Is(input) {
		return input, func() {}
	}

	terminal, err := os.Open(controllingTerminal)
	if err != nil {
		return input, func() {}
	}

	return terminal, func() { _ = terminal.Close() }
}

func Raw(terminal *os.File, screen io.Writer) (func(), error) {
	if !Is(terminal) || !Is(screen) {
		return nil, ErrNotTerminal
	}

	terminalState, err := term.MakeRaw(int(terminal.Fd()))
	if err != nil {
		return nil, err
	}

	_, _ = io.WriteString(screen, key.Enable)

	return func() {
		_, _ = io.WriteString(screen, key.Disable)
		_ = term.Restore(int(terminal.Fd()), terminalState)
	}, nil
}
