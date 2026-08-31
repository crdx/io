package startup

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/internal/util"
	"crdx.org/io/session"
)

var startedAt = time.Now()

func Elapsed() time.Duration {
	return time.Since(startedAt)
}

func Wait(run func()) {
	waitedFrom := time.Now()
	run()
	startedAt = startedAt.Add(time.Since(waitedFrom))
}

type Info struct {
	Session       string `json:"session,omitempty"`
	PromptBytes   int    `json:"prompt,omitempty"`
	ProjectSkills int    `json:"project_skills,omitempty"`
	GlobalSkills  int    `json:"global_skills,omitempty"`
	Snippets      int    `json:"snippets,omitempty"`
	ToolBytes     int    `json:"tools,omitempty"`
}

// NewEvent records startup facts for live display and later replay.
func NewEvent(elapsedTime time.Duration, info Info) agent.Event {
	facts, err := json.Marshal(info)
	if err != nil {
		return agent.Event{Kind: agent.StartupEvent, Took: elapsedTime}
	}

	return agent.Event{Kind: agent.StartupEvent, Took: elapsedTime, State: facts}
}

const (
	sizedEmojiCells    = 4
	bannerLeftPadding  = 1
	bannerGap          = 2
	textSizingMetadata = "s=2:w=2"
)

// RenderEvent renders a recorded startup event for the available terminal width.
func RenderEvent(event agent.Event, columns int, isTextSizingSupported bool) string {
	var info Info
	if len(event.State) > 0 {
		if err := json.Unmarshal(event.State, &info); err != nil {
			return ""
		}
	}

	return RenderBanner(event.Took, false, info, columns, isTextSizingSupported)
}

// RenderBanner renders the startup summary for the available terminal width.
func RenderBanner(elapsedTime time.Duration, wasResumed bool, info Info, columns int, isTextSizingSupported bool) string {
	if wasResumed {
		return ""
	}

	emoji := session.Emoji(info.Session)
	heading := renderHeading(elapsedTime, info, false)
	headingRoom := columns - bannerLeftPadding - sizedEmojiCells - bannerGap
	if emoji == "" || !isTextSizingSupported || style.Width(heading) > headingRoom {
		return renderSentence(elapsedTime, info)
	}

	leftPadding := strings.Repeat(" ", bannerLeftPadding)
	indent := "\x1b[" + strconv.Itoa(bannerLeftPadding+sizedEmojiCells) + "C" + strings.Repeat(" ", bannerGap)
	return leftPadding + sizedEmoji(emoji) + style.Subtle(strings.Repeat(" ", bannerGap)) + heading + "\n" + indent + renderDetails(info, "", "")
}

func renderSentence(elapsedTime time.Duration, info Info) string {
	return renderHeading(elapsedTime, info, true) + renderDetails(info, " with ", ".")
}

func renderHeading(elapsedTime time.Duration, info Info, shouldIncludeEmoji bool) string {
	var line strings.Builder

	_, _ = line.WriteString(style.Subtle("Agent"))
	if info.Session != "" {
		_, _ = line.WriteString(style.Subtle(" ") + style.Normal(info.Session))
		if emoji := session.Emoji(info.Session); shouldIncludeEmoji && emoji != "" {
			_, _ = line.WriteString(style.Subtle(" ") + style.Normal(emoji))
		}
	}
	_, _ = line.WriteString(style.Subtle(" ready in ") + startupDuration(elapsedTime))
	return line.String()
}

func renderDetails(info Info, introduction string, conclusion string) string {
	var line strings.Builder

	if introduction != "" {
		_, _ = line.WriteString(style.Subtle(introduction))
	}
	_, _ = line.WriteString(style.Normal(strconv.Itoa(info.ProjectSkills + info.GlobalSkills)))
	_, _ = line.WriteString(style.Subtle(" skills, "))
	_, _ = line.WriteString(style.Normal(strconv.Itoa(info.Snippets)))
	_, _ = line.WriteString(style.Subtle(" snippets, and "))
	_, _ = line.WriteString(startupContextTokens(info))
	_, _ = line.WriteString(style.Subtle(" of context" + conclusion))
	return line.String()
}

func sizedEmoji(emoji string) string {
	return "\x1b]66;" + textSizingMetadata + ";" + emoji + "\x1b\\"
}

func startupDuration(elapsedTime time.Duration) string {
	var field startupLine
	field.quantity(timeTaken(elapsedTime), false)
	return field.String()
}

func startupContextTokens(info Info) string {
	bytes := info.PromptBytes + info.ToolBytes

	var field startupLine
	field.quantity(util.FormatTokenEstimate(bytes), false)
	return field.String()
}

type startupLine struct {
	strings.Builder
}

func (self *startupLine) dim(text string) {
	_, _ = self.WriteString(style.Subtle(text))
}

func (self *startupLine) normal(text string) {
	_, _ = self.WriteString(style.Normal(text))
}

func (self *startupLine) quantity(text string, isUnitNormal bool) {
	numberStart := 0
	if strings.HasPrefix(text, "~") {
		self.dim("~")
		numberStart++
	}

	at := numberStart
	for at < len(text) && ((text[at] >= '0' && text[at] <= '9') || text[at] == '.') {
		at++
	}

	if at == numberStart {
		self.dim(text[numberStart:])
		return
	}
	if isUnitNormal {
		self.normal(text[numberStart:])
		return
	}

	self.normal(text[numberStart:at])
	self.dim(text[at:])
}

func timeTaken(elapsedTime time.Duration) string {
	if elapsedTime < time.Millisecond {
		return elapsedTime.Round(time.Microsecond).String()
	}

	return elapsedTime.Round(time.Millisecond).String()
}
