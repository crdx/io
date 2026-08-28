package transcript

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/internal/util/strutil"
	"crdx.org/io/tool"
)

const (
	toolResultPreviewLines = 3
	toolResultPreviewBytes = 1 << 10
)

var safeSyntax = regexp.MustCompile(`^[A-Za-z0-9_+.-]+$`)

// Meta identifies the conversation rendered into a transcript.
type Meta struct {
	Name, Model, Effort, Provider, Workspace string
	Started                                  time.Time
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
		_, err = fmt.Fprintf(file, "# Conversation\n\n- **Session:** `%s`\n- **Started:** `%s`\n- **Model:** `%s`\n- **Effort:** `%s`\n- **Provider:** `%s`\n- **Workspace:** `%s`\n\n", meta.Name, meta.Started.UTC().Format(time.RFC3339Nano), meta.Model, meta.Effort, meta.Provider, meta.Workspace)
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
	var output document
	output.paragraph("## " + title(event.Kind))
	output.paragraph("> " + at.UTC().Format(time.RFC3339Nano))

	switch event.Kind {
	case agent.UserMessageEvent, agent.ModelReasoningEvent, agent.ModelMessageEvent:
		output.fence(event.Text, "")
	case agent.HarnessMessageEvent:
		output.field("Status", string(event.Status))
		output.fence(event.Text, "")
	case agent.ToolCallRequestEvent:
		output.field("ID", event.ID)
		output.field("Name", event.Name)
		output.flag("Read only", event.ReadOnly)
		output.field("Emphasis", describeEmphasis(event.Emphasis))
		if event.Arguments != "" {
			output.paragraph("**Arguments**")
			output.fence(event.Arguments, "json")
		}
		if event.Subject != "" {
			output.paragraph("**Subject**")
			output.fence(event.Subject, emphasisLanguage(event.Emphasis))
		}
		if event.Note != "" {
			output.paragraph("**Qualifier**")
			output.fence(event.Note, "")
		}
	case agent.ToolCallResultEvent:
		output.field("ID", event.ID)
		output.field("Name", event.Name)
		output.field("Status", string(event.Status))
		if event.Took != 0 {
			output.field("Duration", event.Took.String())
		}
		output.field("Qualifier", event.Note)
		output.field("Emphasis", describeEmphasis(event.Emphasis))
		if event.Stats != nil {
			stats, err := json.Marshal(event.Stats)
			if err != nil {
				return err
			}
			output.paragraph("**Stats**")
			output.fence(string(stats), "json")
		}
		if event.Text != "" {
			output.toolResultPreview(event.Text, emphasisLanguage(event.Emphasis))
		}
	case agent.StateChangeEvent:
		output.field("ID", event.ID)
		output.field("Name", event.Name)
		output.paragraph("**State**")
		output.fence(string(event.State), "json")
	case caps.ModeChange:
		output.field("Swapped", event.Name)
		output.field("Caps", event.Text)

		if notice, said := caps.ModeNotice(event); said {
			output.fence(notice, "")
		}
	case agent.InterruptionEvent:
		output.paragraph(interruptionSentence(event.Text))
	case agent.RetryingEvent:
		output.field("Attempt", strconv.Itoa(event.Attempt))
		output.field("Waited", event.Took.String())
		output.field("ID", event.ID)
		output.field("Name", event.Name)
		output.fence(event.Text, "")
		if event.Arguments != "" {
			output.paragraph("**Arguments**")
			output.fence(event.Arguments, "")
		}
	case agent.FailureEvent:
		output.fence(event.Text, "")
	}

	_, err := self.file.WriteString(output.String())
	return err
}

func interruptionSentence(reason string) string {
	if reason == "" {
		return "The turn was interrupted."
	}

	return "The turn was interrupted because " + reason + "."
}

func describeEmphasis(emphasis tool.Emphasis) string {
	if emphasis.Kind == "" {
		return ""
	}

	return string(emphasis.Kind) + " " + emphasis.Value
}

func emphasisLanguage(emphasis tool.Emphasis) string {
	if emphasis.Kind == tool.EmphasisSyntax {
		return emphasis.Value
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
	case agent.StartupEvent:
		return "Startup"
	case agent.UserMessageEvent:
		return "User"
	case agent.HarnessMessageEvent:
		return "Notice"
	case agent.ModelReasoningEvent:
		return "Reasoning"
	case agent.ModelMessageEvent:
		return "Assistant"
	case agent.ToolCallRequestEvent:
		return "Tool call"
	case agent.ToolCallResultEvent:
		return "Tool result"
	case agent.StateChangeEvent:
		return "State"
	case caps.ModeChange:
		return "Mode"
	case agent.InterruptionEvent:
		return "Interrupted"
	case agent.RetryingEvent:
		return "Retrying"
	case agent.FailureEvent:
		return "Failure"
	default:
		return string(kind)
	}
}

type document struct {
	text          strings.Builder
	isInFieldList bool
}

func (self *document) String() string {
	return self.text.String() + "\n"
}

func (self *document) field(label, value string) {
	if value == "" {
		return
	}
	self.openField()
	fmt.Fprintf(&self.text, "- **%s:** `%s`\n", label, value)
}

func (self *document) flag(label string, value bool) {
	self.openField()
	fmt.Fprintf(&self.text, "- **%s:** `%t`\n", label, value)
}

func (self *document) paragraph(text string) {
	self.openBlock()
	self.text.WriteString(text)
	self.text.WriteString("\n")
}

func (self *document) openField() {
	if !self.isInFieldList {
		self.openBlock()
		self.isInFieldList = true
	}
}

func (self *document) openBlock() {
	self.isInFieldList = false
	if self.text.Len() > 0 {
		self.text.WriteString("\n")
	}
}

func (self *document) toolResultPreview(value, syntax string) {
	lines := strutil.Lines(value)
	preview := strings.Join(lines[:min(len(lines), toolResultPreviewLines)], "\n")
	if len(preview) > toolResultPreviewBytes {
		end := toolResultPreviewBytes
		for !utf8.RuneStart(preview[end]) {
			end--
		}
		preview = preview[:end]
	}
	self.paragraph("**Output preview (first 3 lines, up to 1 KiB)**")
	self.fence(preview, syntax)
	self.paragraph("Full output: [`session.jsonl`](session.jsonl), in the matching result event's `event.text` field.")
}

func (self *document) fence(value, syntax string) {
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
	self.openBlock()
	fmt.Fprintf(&self.text, "%s%s\n%s\n%s\n", fence, syntax, value, fence)
}
