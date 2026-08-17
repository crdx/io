// Package sse reads JSON payloads from server-sent event streams. It joins data lines without
// newlines and ignores event, id, and retry fields.
package sse

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

// ErrTruncated reports a stream that ended before Step accepted a payload.
var ErrTruncated = errors.New("the stream ended before the response did")

// Step handles a payload and reports when reading should stop.
type Step func(payload string) (bool, error)

// Read passes each frame to step until it accepts one. EOF before acceptance returns ErrTruncated.
// Only a newline ends a line; a bare carriage return remains in the payload.
func Read(body io.Reader, step Step) error {
	reader := bufio.NewReader(body)

	var data strings.Builder

	for {
		line, err := reader.ReadString('\n')
		eof := errors.Is(err, io.EOF)

		if err != nil && !eof {
			return err
		}

		text := strings.TrimRight(line, "\r\n")

		if field, hasData := strings.CutPrefix(text, "data:"); hasData {
			data.WriteString(strings.TrimSpace(field))
		}

		if text == "" || eof {
			payload := data.String()
			data.Reset()

			if payload != "" {
				switch done, err := step(payload); {
				case err != nil:
					return err
				case done:
					return nil
				}
			}
		}

		if eof {
			return ErrTruncated
		}
	}
}
