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

// Tools wraps every tool so none of them can answer with more than limit bytes.
func Tools(subjects []tool.Tool, limit int) []tool.Tool {
	wrappedTools := make([]tool.Tool, len(subjects))

	for i, subject := range subjects {
		wrappedTools[i] = Tool(subject, limit)
	}

	return wrappedTools
}

// Tool wraps one tool so its output is cut at limit bytes.
func Tool(inner tool.Tool, limit int) tool.Tool {
	return truncatedTool{Tool: inner, limit: limit}
}

type truncatedTool struct {
	tool.Tool

	limit int
}

func (self truncatedTool) Parse(arguments string) (tool.ToolCall, error) {
	call, err := self.Tool.Parse(arguments)
	if err != nil {
		return nil, err
	}

	return truncatedToolCall{ToolCall: call, limit: self.limit}, nil
}

type truncatedToolCall struct {
	tool.ToolCall

	limit int
}

func (self truncatedToolCall) Exec(ctx context.Context) (tool.ToolCallResult, error) {
	result, err := self.ToolCall.Exec(ctx)
	cappedOutput, returnedBytes, totalBytes := outputWithSizes(result.Output, self.limit)
	result.Output = cappedOutput

	if result.Stats.Kind == tool.StatsResources || returnedBytes < totalBytes {
		result.Stats.Bytes = int64(returnedBytes)
		result.Stats.TotalBytes = int64(totalBytes)
		result.Stats.Truncated = result.Stats.Truncated || returnedBytes < totalBytes
	}

	return result, err
}

// Output caps one reply at limit bytes, cutting at a line boundary where there is one and a rune
// boundary where there is not. The cut is said out loud, and the whole of it written to a file the
// notice names.
func Output(output string, limit int) string {
	cappedOutput, _, _ := outputWithSizes(output, limit)
	return cappedOutput
}

func outputWithSizes(output string, limit int) (string, int, int) {
	if len(output) <= limit {
		return output, len(output), len(output)
	}

	end := limit

	if newline := strings.LastIndexByte(output[:limit], '\n'); newline > 0 {
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
