package truncate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"crdx.org/io/tool"
)

// Limit is the most a tool may hand back before it is cut.
const Limit = 32 * 1024

// Tools wraps every tool so none of them can answer with more than Limit.
func Tools(subjects []tool.Tool) []tool.Tool {
	wrapped := make([]tool.Tool, len(subjects))

	for index, subject := range subjects {
		wrapped[index] = Tool(subject)
	}

	return wrapped
}

// Tool wraps one tool so its output is capped.
func Tool(subject tool.Tool) tool.Tool {
	return capped{Tool: subject}
}

type capped struct {
	tool.Tool
}

func (self capped) Parse(arguments string) (tool.Call, error) {
	call, err := self.Tool.Parse(arguments)
	if err != nil {
		return nil, err
	}

	return cappedCall{Call: call}, nil
}

type cappedCall struct {
	tool.Call
}

func (self cappedCall) Exec() (string, error) {
	output, err := self.Call.Exec()
	if err != nil {
		return output, err
	}

	return Output(output), nil
}

// Output caps one reply, cutting at a line boundary where there is one and a rune boundary where
// there is not. The cut is said out loud, and the whole of it written to a file the notice names.
func Output(output string) string {
	if len(output) <= Limit {
		return output
	}

	end := Limit

	if newline := strings.LastIndexByte(output[:Limit], '\n'); newline > 0 {
		end = newline
	} else {
		for end > 0 && !utf8.RuneStart(output[end]) {
			end--
		}
	}

	path, err := save(output)
	if err != nil {
		return fmt.Sprintf(
			"%s\n\n[truncated at %d bytes of %d; the rest could not be saved: %v]",
			output[:end], end, len(output), err,
		)
	}

	return fmt.Sprintf(
		"%s\n\n[truncated at %d bytes of %d; the whole of it is in %s]",
		output[:end], end, len(output), path,
	)
}

func save(output string) (string, error) {
	file, err := os.CreateTemp(os.TempDir(), "io-output-*.txt")
	if err != nil {
		return "", err
	}

	defer func() { _ = file.Close() }()

	if _, err := file.WriteString(output); err != nil {
		return "", err
	}

	return filepath.Clean(file.Name()), nil
}
