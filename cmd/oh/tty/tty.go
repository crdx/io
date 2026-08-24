package tty

import (
	"errors"
	"io"
	"os"

	"golang.org/x/term"

	"crdx.org/io/cmd/oh/key"
)

// ErrNotTerminal is either end of the conversation not being a terminal, which is the harness being
// piped or redirected rather than talked to.
var ErrNotTerminal = errors.New("not a terminal")

// Is says whether something is a terminal, which anything that is not a file is not.
func Is(stream any) bool {
	file, ok := stream.(*os.File)

	return ok && term.IsTerminal(int(file.Fd()))
}

// Raw enables raw input and the keyboard protocol, returning a restore function. Both input and
// output must be terminals.
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
