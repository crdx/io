package transcript

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/internal/util"
	"crdx.org/io/internal/util/strutil"
	"crdx.org/io/tool"
)

const (
	toolResultPreviewLines = 3
	toolResultPreviewBytes = 1 << 10

	formattedBytePrecision = 3
	maximumInlineSubject   = 120

	partSeparator = " · "
	resultArrow   = "→ "

	patience    = 5 * time.Second
	noTimeAtAll = "0s"
)

var (
	safeSyntax = regexp.MustCompile(`^[A-Za-z0-9_+.-]+$`)
	safeID     = regexp.MustCompile(`^[A-Za-z0-9_.:|-]+$`)
)

// Meta identifies the conversation rendered into a transcript.
type Meta struct {
	Name, Model, Effort, Provider, Workspace string
	StartedAt                                time.Time
}

// Recorder appends conversation events as Markdown.
type Recorder struct {
	file       *os.File
	startedAt  time.Time
	lastCallID string
}

// Open opens or creates a Markdown transcript.
func Open(path string, meta Meta) (*Recorder, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // the parent store supplies the fixed bundle path
	if err != nil {
		return nil, err
	}
	recorder := &Recorder{file: file, startedAt: meta.StartedAt}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if info.Size() == 0 {
		_, err = fmt.Fprintf(file, "# Conversation\n\n- **Session:** `%s`\n- **Started:** `%s`\n- **Model:** `%s`\n- **Effort:** `%s`\n- **Provider:** `%s`\n- **Workspace:** `%s`\n\n", meta.Name, meta.StartedAt.UTC().Format(time.RFC3339Nano), meta.Model, meta.Effort, meta.Provider, meta.Workspace)
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
	if event.Kind == agent.StateChangeEvent {
		return nil
	}

	isAnswerToTheCallAbove := event.Kind == agent.ToolCallResultEvent && event.ID != "" && event.ID == self.lastCallID
	self.lastCallID = ""
	if event.Kind == agent.ToolCallRequestEvent {
		self.lastCallID = event.ID
	}

	var output document
	if isAnswerToTheCallAbove {
		output.paragraph(joinParts(resultArrow+outcome(event), self.offset(at)))
	} else {
		output.paragraph("## " + joinParts(append(heading(event), self.offset(at))...))
	}

	switch event.Kind {
	case agent.UserMessageEvent, agent.ModelReasoningEvent, agent.ModelMessageEvent, agent.HarnessMessageEvent, agent.FailureEvent:
		output.fence(event.Text, "")
	case agent.ToolCallRequestEvent:
		if event.Subject != "" && inlineSubject(event) == "" {
			output.fence(event.Subject, emphasisLanguage(event.Emphasis))
		} else if event.Subject == "" && event.Arguments != "" {
			output.fence(event.Arguments, "json")
		}
	case agent.ToolCallResultEvent:
		if event.Text != "" {
			output.toolResultPreview(event.ID, event.Text, emphasisLanguage(event.Emphasis))
		}
	case caps.ModeChange:
		if notice, said := caps.ModeNotice(event); said {
			output.fence(notice, "")
		}
	case agent.InterruptionEvent:
		output.paragraph(interruptionSentence(event.Text))
	case agent.RetryingEvent:
		output.fence(event.Text, "")
		if event.Arguments != "" {
			output.fence(event.Arguments, "")
		}
	case agent.StartupEvent, agent.StateChangeEvent:
	}

	_, err := self.file.WriteString(output.String())
	return err
}

func heading(event agent.Event) []string {
	name := title(event.Kind)
	if event.Name != "" && namesACall(event.Kind) {
		name += " — " + event.Name
	}

	switch event.Kind {
	case agent.ToolCallRequestEvent:
		if event.Note != "" {
			name += " (" + event.Note + ")"
		}

		return []string{name, code(inlineSubject(event))}
	case agent.ToolCallResultEvent:
		return []string{name, outcome(event)}
	case agent.HarnessMessageEvent:
		return []string{name, string(event.Status)}
	case caps.ModeChange:
		return []string{name, event.Text, prefixed("toggled ", event.Name)}
	case agent.RetryingEvent:
		return []string{name, "attempt " + strconv.Itoa(event.Attempt), prefixed("waited ", util.CompactDuration(event.Took))}
	case agent.StartupEvent, agent.UserMessageEvent, agent.ModelReasoningEvent, agent.ModelMessageEvent,
		agent.StateChangeEvent, agent.InterruptionEvent, agent.FailureEvent:
	}

	return []string{name}
}

func namesACall(kind agent.Kind) bool {
	return kind == agent.ToolCallRequestEvent || kind == agent.ToolCallResultEvent || kind == agent.RetryingEvent
}

func inlineSubject(event agent.Event) string {
	isTooLongToRead := len([]rune(event.Subject)) > maximumInlineSubject
	if emphasisLanguage(event.Emphasis) != "" || isTooLongToRead || strings.ContainsAny(event.Subject, "\n`") {
		return ""
	}

	return event.Subject
}

func outcome(event agent.Event) string {
	status := string(event.Status)
	if event.Took >= patience {
		status = strings.TrimSpace(status + " in " + util.CompactDuration(event.Took))
	}

	var parts []string
	for _, part := range []string{status, event.Note, measurements(event.Stats)} {
		if part != "" {
			parts = append(parts, part)
		}
	}

	return strings.Join(parts, ", ")
}

func measurements(stats *tool.Stats) string {
	if stats == nil {
		return ""
	}

	var parts []string
	if stats.Lines > 0 {
		parts = append(parts, plural(stats.Lines, "line"))
	}
	if stats.Bytes > 0 {
		size := util.FormatBytes(stats.Bytes, formattedBytePrecision)
		if stats.TotalBytes > stats.Bytes {
			size += " of " + util.FormatBytes(stats.TotalBytes, formattedBytePrecision)
		}
		parts = append(parts, size)
	}
	if stats.AddedLines > 0 || stats.RemovedLines > 0 {
		parts = append(parts, fmt.Sprintf("+%d −%d", stats.AddedLines, stats.RemovedLines))
	}
	if stats.EstimatedTokens > 0 {
		parts = append(parts, util.FormatEstimatedTokenCount(stats.EstimatedTokens))
	}
	if cpuTime := util.CompactDuration(stats.CPUTime); stats.CPUTime > 0 && cpuTime != noTimeAtAll {
		parts = append(parts, cpuTime+" CPU")
	}
	if stats.PeakMemory > 0 {
		parts = append(parts, util.FormatBytes(stats.PeakMemory, formattedBytePrecision)+" peak")
	}
	if stats.IsTruncated {
		parts = append(parts, "truncated")
	}

	return strings.Join(parts, ", ")
}

func joinParts(parts ...string) string {
	var present []string
	for _, part := range parts {
		if part != "" {
			present = append(present, part)
		}
	}

	return strings.Join(present, partSeparator)
}

func prefixed(prefix string, value string) string {
	if value == "" {
		return ""
	}

	return prefix + value
}

func code(value string) string {
	if value == "" {
		return ""
	}

	return "`" + value + "`"
}

func plural(count int64, noun string) string {
	if count == 1 {
		return "1 " + noun
	}

	return fmt.Sprintf("%d %ss", count, noun)
}

func interruptionSentence(reason string) string {
	if reason == "" {
		return "The turn was interrupted."
	}

	return "The turn was interrupted because " + reason + "."
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

func (self *Recorder) offset(at time.Time) string {
	if self.startedAt.IsZero() {
		return ""
	}

	return "+" + util.CompactDuration(at.Sub(self.startedAt))
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
	case caps.ModeChange:
		return "Mode"
	case agent.InterruptionEvent:
		return "Interrupted"
	case agent.RetryingEvent:
		return "Retrying"
	case agent.FailureEvent:
		return "Failure"
	case agent.StateChangeEvent:
		return string(kind)
	default:
		return string(kind)
	}
}

type document struct {
	text strings.Builder
}

func (self *document) String() string {
	return self.text.String() + "\n"
}

func (self *document) paragraph(text string) {
	self.openBlock()
	self.text.WriteString(text)
	self.text.WriteString("\n")
}

func (self *document) openBlock() {
	if self.text.Len() > 0 {
		self.text.WriteString("\n")
	}
}

func (self *document) toolResultPreview(id string, value string, syntax string) {
	lines := strutil.Lines(value)
	preview := strings.Join(lines[:min(len(lines), toolResultPreviewLines)], "\n")
	if len(preview) > toolResultPreviewBytes {
		end := toolResultPreviewBytes
		for !utf8.RuneStart(preview[end]) {
			end--
		}
		preview = preview[:end]
	}
	if preview == strings.TrimSuffix(value, "\n") {
		self.fence(preview, syntax)
		return
	}

	self.paragraph("**Output preview (first 3 lines, up to 1 KiB)**")
	self.fence(preview, syntax)
	if !safeID.MatchString(id) {
		self.paragraph("Full output: [`session.jsonl`](session.jsonl), in the matching result event's `event.text` field.")
		return
	}
	self.paragraph("Full output, from the session directory:")
	self.fence(fmt.Sprintf("jq -r 'select(.event.kind == %q and .event.id == %q) | .event.text' session.jsonl", agent.ToolCallResultEvent, id), "sh")
}

func (self *document) fence(value string, syntax string) {
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
