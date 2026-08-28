package tty

import (
	"errors"
	"io"
	"os"

	"golang.org/x/term"
)

var errInputCancelled = errors.New("input cancelled")

func ReadMaskedLine(input *os.File, output io.Writer) (string, error) {
	terminalState, err := term.MakeRaw(int(input.Fd()))
	if err != nil {
		return "", err
	}
	defer func() { _ = term.Restore(int(input.Fd()), terminalState) }()

	return readMaskedLine(input, output)
}

func readMaskedLine(input io.Reader, output io.Writer) (string, error) {
	var value []byte
	var character [1]byte

	for {
		read, err := input.Read(character[:])

		if read == 1 {
			switch character[0] {
			case '\r', '\n':
				_, writeError := io.WriteString(output, "\r\n")

				return string(value), writeError
			case 3:
				return "", errInputCancelled
			case '\b', 0x7f:
				var writeError error
				if value, writeError = eraseMaskedByte(value, output); writeError != nil {
					return "", writeError
				}
			default:
				var writeError error
				if value, writeError = maskByte(value, character[0], output); writeError != nil {
					return "", writeError
				}
			}
		}

		if err != nil {
			return "", err
		}
	}
}

func eraseMaskedByte(value []byte, output io.Writer) ([]byte, error) {
	if len(value) == 0 {
		return value, nil
	}

	if _, err := io.WriteString(output, "\b \b"); err != nil {
		return value, err
	}

	return value[:len(value)-1], nil
}

func maskByte(value []byte, character byte, output io.Writer) ([]byte, error) {
	if character < ' ' {
		return value, nil
	}

	if _, err := io.WriteString(output, "*"); err != nil {
		return value, err
	}

	return append(value, character), nil
}
