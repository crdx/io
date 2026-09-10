package painter

import (
	"slices"
	"strings"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/jobrecord"
	"crdx.org/io/cmd/oh/link"
	"crdx.org/io/cmd/oh/markdown"
	"crdx.org/io/cmd/oh/pathgrant"
	"crdx.org/io/cmd/oh/portgrant"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/turn"
	"crdx.org/io/cmd/oh/width"
	"crdx.org/io/internal/util/strutil"
)

const (
	unsentMark              = "⏳"
	harnessMark             = "🤖"
	unwrappedPreviewColumns = 1 << 16
)

func submittedMarker(isSent bool) string {
	if isSent {
		return harnessMark + " "
	}

	return unsentMark + " "
}

func RenderQueuedMessages(messages []string, columns int, shouldRenderHyperlinks bool, roots link.Roots) []string {
	if len(messages) == 0 {
		return nil
	}

	rows := make([]string, 0, len(messages)+2)
	rows = append(rows, renderQueuedRow("", columns))

	for _, message := range messages {
		summary := summariseQueuedMessage(message, shouldRenderHyperlinks, roots)
		rows = append(rows, renderQueuedRow(unsentMark+" "+summary, columns))
	}

	return append(rows, renderQueuedRow("", columns))
}

func summariseQueuedMessage(message string, shouldRenderHyperlinks bool, roots link.Roots) string {
	firstLine := strutil.FirstLine(message)

	var renderedLines []string
	if shouldRenderHyperlinks {
		renderedLines = markdown.RenderWithHyperlinksUnder(strutil.StripControl(firstLine), unwrappedPreviewColumns, roots)
	} else {
		renderedLines = markdown.Render(strutil.StripControl(firstLine), unwrappedPreviewColumns)
	}

	summary := firstLine
	if len(renderedLines) > 0 {
		summary = renderedLines[0]
	}

	if strings.Contains(strings.TrimSpace(message), "\n") {
		summary += width.Ellipsis
	}

	return summary
}

func renderQueuedRow(text string, columns int) string {
	row := ""
	if text != "" {
		row = width.Elide(" "+text, columns)
	}

	if room := columns - style.Width(row); room > 0 {
		row += strings.Repeat(" ", room)
	}

	return style.User(row)
}

type PendingMessages struct {
	messages               []string
	pathRoots              link.Roots
	isSent                 bool
	shouldRenderHyperlinks bool
}

func NewPendingMessages(messages []string, shouldRenderHyperlinks bool, pathRoots link.Roots) *PendingMessages {
	return &PendingMessages{
		messages:               slices.Clone(messages),
		pathRoots:              pathRoots,
		shouldRenderHyperlinks: shouldRenderHyperlinks,
	}
}

func (self *PendingMessages) Replace(messages []string) {
	self.messages = slices.Clone(messages)
}

func (self *PendingMessages) MarkSent() {
	self.isSent = true
}

func (self *PendingMessages) Rows(columns int) []string {
	var rows []string

	for i, message := range self.messages {
		if i > 0 {
			rows = append(rows, "")
		}
		rows = append(rows, strings.Split(self.render(message, columns), "\n")...)
	}

	return rows
}

func (self *PendingMessages) render(message string, columns int) string {
	return renderSubmittedMessage(
		message, columns, self.shouldRenderHyperlinks, self.pathRoots, submittedMarker(self.isSent),
	)
}

func HarnessNotice(event agent.Event) (string, bool) {
	switch event.Kind {
	case caps.ModeChange:
		return caps.ModeNotice(event)
	case caps.JobStop:
		return caps.JobStopNotice(event)
	case jobrecord.Ended:
		return jobrecord.EndedNotice(event)
	case jobrecord.EndedWithSession:
		return jobrecord.EndedWithSessionNotice(event)
	case pathgrant.Change:
		return pathgrant.Notice(event)
	case portgrant.SandboxToHostChange:
		return portgrant.SandboxToHostNotice(event)
	case portgrant.HostToSandboxChange:
		return portgrant.HostToSandboxNotice(event)
	case turn.HarnessPoke:
		return turn.PokeNotice(event)
	case agent.StartupEvent, agent.UserMessageEvent, agent.SilentTurnEvent, agent.CacheRebuildEvent, agent.PrefixRewriteEvent,
		agent.ModelReasoningEvent, agent.ModelMessageEvent, agent.ToolCallRequestEvent,
		agent.ToolCallResultEvent, agent.StateChangeEvent, agent.InterruptionEvent,
		agent.RetryingEvent, agent.FailureEvent:
		return "", false
	}
	return "", false
}
