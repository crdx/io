package truncate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"crdx.org/io/internal/util"
	"crdx.org/io/tool"
)

// Limit is the most a tool may hand back before it is cut.
const Limit = 32 * 1024

// Tools wraps every tool so none of them can answer with more than Limit.
func Tools(subjects []tool.Tool) []tool.Tool {
	wrappedTools := make([]tool.Tool, len(subjects))

	for index, subject := range subjects {
		wrappedTools[index] = Tool(subject)
	}

	return wrappedTools
}

// Tool wraps one tool so its output is capped.
func Tool(subject tool.Tool) tool.Tool {
	return capped{Tool: subject}
}

type capped struct {
	tool.Tool // the tool whose output is capped
}

func (self capped) Parse(arguments string) (tool.Call, error) {
	call, err := self.Tool.Parse(arguments)
	if err != nil {
		return nil, err
	}

	return &cappedCall{Call: call}, nil
}

type cappedCall struct {
	tool.Call // the call whose output is capped

	statistics tool.Statistics
	measured   bool
}

func (self *cappedCall) Focus() string {
	if focusedCall, ok := self.Call.(tool.FocusedCall); ok {
		return focusedCall.Focus()
	}

	return ""
}

func (self *cappedCall) Syntax() string {
	if syntaxCall, ok := self.Call.(tool.SyntaxCall); ok {
		return syntaxCall.Syntax()
	}

	return ""
}

func (self *cappedCall) Statistics() (tool.Statistics, bool) {
	return self.statistics, self.measured
}

func (self *cappedCall) Image() (tool.Image, bool) { return tool.AttachedImage(self.Call) }

func (self *cappedCall) Exec(ctx context.Context) (string, error) {
	output, err := self.Call.Exec(ctx)
	cappedOutput, returnedBytes, totalBytes := outputWithSizes(output)

	self.statistics, self.measured = tool.Stats(self.Call)
	if self.measured && (self.statistics.Kind == tool.StatsResources || returnedBytes < totalBytes) {
		self.statistics.Bytes = int64(returnedBytes)
		self.statistics.TotalBytes = int64(totalBytes)
		self.statistics.Truncated = self.statistics.Truncated || returnedBytes < totalBytes
	}

	return cappedOutput, err
}

// Output caps one reply, cutting at a line boundary where there is one and a rune boundary where
// there is not. The cut is said out loud, and the whole of it written to a file the notice names.
func Output(output string) string {
	cappedOutput, _, _ := outputWithSizes(output)
	return cappedOutput
}

func outputWithSizes(output string) (string, int, int) {
	if len(output) <= Limit {
		return output, len(output), len(output)
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
			"%s\n\n[truncated at %s of %s; the rest could not be saved: %v]",
			output[:end], util.FormatBytes(end, 3), util.FormatBytes(len(output), 3), err,
		), end, len(output)
	}

	return fmt.Sprintf(
		"%s\n\n[truncated at %s of %s; the whole of it is in %s]",
		output[:end], util.FormatBytes(end, 3), util.FormatBytes(len(output), 3), path,
	), end, len(output)
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
