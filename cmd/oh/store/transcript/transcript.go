// Package transcript renders a durable Markdown conversation transcript.
package transcript

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"crdx.org/io/agent"
	"crdx.org/io/internal/strutil"
	"crdx.org/io/tool"
)

const (
	toolResultPreviewLines = 3
	toolResultPreviewBytes = 1 << 10
)

var safeSyntax = regexp.MustCompile(`^[A-Za-z0-9_+.-]+$`)

// Meta identifies the conversation rendered into a transcript.
type Meta struct {
	ID, Model, Effort, Provider, Workspace string
	Started                                time.Time
}

// Recorder appends conversation events as Markdown.
type Recorder struct {
	file *os.File
}

// Open opens or creates a Markdown transcript.
func Open(path string, meta Meta) (*Recorder, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // the parent store supplies the fixed bundle path
	if err != nil {
		return nil, err
	}
	recorder := &Recorder{file: file}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if info.Size() == 0 {
		_, err = fmt.Fprintf(file, "# Conversation\n\n- **ID:** `%s`\n- **Started:** `%s`\n- **Model:** `%s`\n- **Effort:** `%s`\n- **Provider:** `%s`\n- **Workspace:** `%s`\n\n", meta.ID, meta.Started.UTC().Format(time.RFC3339Nano), meta.Model, meta.Effort, meta.Provider, meta.Workspace)
		if err != nil {
			_ = file.Close()
			return nil, err
		}
	}
	return recorder, nil
}

// Event appends one portable conversation event.
func (self *Recorder) Event(at time.Time, event agent.Event) error {
	if self.file == nil {
		return os.ErrClosed
	}
	var output strings.Builder
	fmt.Fprintf(&output, "## %s\n\n> %s\n\n", title(event.Kind), at.UTC().Format(time.RFC3339Nano))

	switch event.Kind {
	case agent.Prompt, agent.Reasoning, agent.Text:
		writeFence(&output, event.Text, "")
	case agent.Call:
		writeField(&output, "ID", event.ID)
		writeField(&output, "Name", event.Name)
		writeBool(&output, "Read only", event.ReadOnly)
		writeField(&output, "Highlight", describeHighlight(event.Highlight))
		if event.Arguments != "" {
			output.WriteString("**Arguments**\n\n")
			writeFence(&output, event.Arguments, "json")
		}
		if event.Render != "" {
			output.WriteString("**Rendering**\n\n")
			writeFence(&output, event.Render, highlightSyntax(event.Highlight))
		}
		if event.Detail != "" {
			output.WriteString("**Detail**\n\n")
			writeFence(&output, event.Detail, "")
		}
	case agent.Result:
		writeField(&output, "ID", event.ID)
		writeField(&output, "Name", event.Name)
		writeBool(&output, "Failed", event.Failed)
		if event.Took != 0 {
			writeField(&output, "Duration", event.Took.String())
		}
		writeField(&output, "Detail", event.Detail)
		writeField(&output, "Highlight", describeHighlight(event.Highlight))
		if event.Stats != nil {
			stats, err := json.Marshal(event.Stats)
			if err != nil {
				return err
			}
			output.WriteString("**Stats**\n\n")
			writeFence(&output, string(stats), "json")
		}
		if event.Text != "" {
			writeToolResultPreview(&output, event.Text, highlightSyntax(event.Highlight))
		}
	case agent.StateEvent:
		writeField(&output, "ID", event.ID)
		writeField(&output, "Name", event.Name)
		output.WriteString("**State**\n\n")
		writeFence(&output, string(event.State), "json")
	case agent.ContextUsage:
		if event.Usage != nil {
			writeField(&output, "Input tokens", fmt.Sprint(event.Usage.InputTokens))
		}
	case agent.Interrupted:
		output.WriteString("The turn was interrupted.\n\n")
	}

	_, err := self.file.WriteString(output.String())
	return err
}

func describeHighlight(highlight tool.Highlight) string {
	if highlight.Kind == "" {
		return ""
	}

	return string(highlight.Kind) + " " + highlight.Value
}

func highlightSyntax(highlight tool.Highlight) string {
	if highlight.Kind == tool.HighlightSyntax {
		return highlight.Value
	}

	return ""
}

// Close closes the transcript.
func (self *Recorder) Close() error {
	if self.file == nil {
		return nil
	}
	err := self.file.Close()
	self.file = nil
	return err
}

func title(kind agent.Kind) string {
	switch kind {
	case agent.Prompt:
		return "User"
	case agent.Reasoning:
		return "Reasoning"
	case agent.Text:
		return "Assistant"
	case agent.Call:
		return "Tool call"
	case agent.Result:
		return "Tool result"
	case agent.StateEvent:
		return "State"
	case agent.ContextUsage:
		return "Context usage"
	case agent.Interrupted:
		return "Interrupted"
	default:
		return string(kind)
	}
}

func writeField(output *strings.Builder, label, value string) {
	if value != "" {
		fmt.Fprintf(output, "- **%s:** `%s`\n", label, value)
	}
}

func writeBool(output *strings.Builder, label string, value bool) {
	fmt.Fprintf(output, "- **%s:** `%t`\n", label, value)
}

func writeToolResultPreview(output *strings.Builder, value, syntax string) {
	lines := strutil.Lines(value)
	preview := strings.Join(lines[:min(len(lines), toolResultPreviewLines)], "\n")
	if len(preview) > toolResultPreviewBytes {
		end := toolResultPreviewBytes
		for !utf8.RuneStart(preview[end]) {
			end--
		}
		preview = preview[:end]
	}
	output.WriteString("**Output preview (first 3 lines, up to 1 KiB)**\n\n")
	writeFence(output, preview, syntax)
	output.WriteString("Full output: [`session.jsonl`](session.jsonl), in the matching result event's `event.text` field.\n\n")
}

func writeFence(output *strings.Builder, value, syntax string) {
	longest := 0
	for _, run := range strings.FieldsFunc(value, func(character rune) bool { return character != '`' }) {
		if len(run) > longest {
			longest = len(run)
		}
	}
	length := max(3, longest+1)
	fence := strings.Repeat("`", length)
	if !safeSyntax.MatchString(syntax) {
		syntax = ""
	}
	fmt.Fprintf(output, "%s%s\n%s\n%s\n\n", fence, syntax, value, fence)
}
