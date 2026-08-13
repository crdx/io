// Package sse reads server-sent event streams, and knows nothing of what any of them are about.
//
// It is a working subset of the standard rather than all of it: the data lines of a frame are
// joined without the newline the standard puts between them, and event, id and retry lines are
// passed over, both of which suit an endpoint that sends one JSON document per frame.
package sse

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

// ErrTruncated is a stream that stopped without the reader ever saying it was finished, which means
// the wire went quiet mid-turn rather than the sender reaching an end.
var ErrTruncated = errors.New("the stream ended before the response did")

// Step is handed each payload as its frame arrives, and returns true where the stream has said
// everything the reader was waiting for.
type Step func(payload string) (bool, error)

// Read hands each frame's payload to step, where a frame is the data lines gathered up since the
// last blank line, and stops as soon as step says it has heard enough. A stream that runs out
// before that happens is an ErrTruncated. Only a newline ends a line here, where the standard also
// counts a carriage return on its own, so a stray one is carried through as part of the payload
// rather than splitting it.
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

		if field, carried := strings.CutPrefix(text, "data:"); carried {
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
