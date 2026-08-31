package truncate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"crdx.org/io/internal/util"
	"crdx.org/io/tool"
)

type Limit struct {
	bytes atomic.Int64
}

func NewLimit(bytes int) *Limit {
	limit := &Limit{}
	limit.Replace(bytes)
	return limit
}

func (self *Limit) GetBytes() int {
	return int(self.bytes.Load())
}

func (self *Limit) Replace(bytes int) {
	self.bytes.Store(int64(bytes))
}

func Tools(subjects []tool.Tool, limit *Limit) []tool.Tool {
	wrappedTools := make([]tool.Tool, len(subjects))

	for i, subject := range subjects {
		wrappedTools[i] = truncatedTool{Tool: subject, getLimit: limit.GetBytes}
	}

	return wrappedTools
}

func Tool(inner tool.Tool, limit int) tool.Tool {
	return truncatedTool{Tool: inner, getLimit: func() int { return limit }}
}

type truncatedTool struct {
	tool.Tool

	getLimit func() int
}

func (self truncatedTool) Parse(arguments string) (tool.ToolCall, error) {
	call, err := self.Tool.Parse(arguments)
	if err != nil {
		return nil, err
	}

	return truncatedToolCall{ToolCall: call, limit: self.getLimit()}, nil
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
		result.Stats.IsTruncated = result.Stats.IsTruncated || returnedBytes < totalBytes
	}

	return result, err
}

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
