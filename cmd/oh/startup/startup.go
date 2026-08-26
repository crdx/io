package startup

import (
	"encoding/json"
	"fmt"
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

type Info struct {
	Session       string `json:"session,omitempty"`
	PromptBytes   int    `json:"prompt,omitempty"`
	ProjectSkills int    `json:"project_skills,omitempty"`
	GlobalSkills  int    `json:"global_skills,omitempty"`
	Snippets      int    `json:"snippets,omitempty"`
	ToolBytes     int    `json:"tools,omitempty"`
}

// NewEvent records startup facts for live display and later replay.
func NewEvent(elapsed time.Duration, info Info) agent.Event {
	facts, err := json.Marshal(info)
	if err != nil {
		return agent.Event{Kind: agent.StartupEvent, Took: elapsed}
	}

	return agent.Event{Kind: agent.StartupEvent, Took: elapsed, State: facts}
}

// RenderEvent renders a recorded startup event.
func RenderEvent(event agent.Event) string {
	var info Info
	if len(event.State) > 0 {
		if err := json.Unmarshal(event.State, &info); err != nil {
			return ""
		}
	}

	return RenderBanner(event.Took, false, info)
}

// RenderBanner renders the startup summary as a sentence.
func RenderBanner(elapsed time.Duration, resumed bool, info Info) string {
	if resumed {
		return ""
	}

	var line strings.Builder

	_, _ = line.WriteString(style.Subtle("Agent"))
	if info.Session != "" {
		_, _ = line.WriteString(style.Subtle(" ") + style.Normal(info.Session))
		if emoji := session.Emoji(info.Session); emoji != "" {
			_, _ = line.WriteString(style.Subtle(" ") + style.Normal(emoji))
		}
	}
	_, _ = line.WriteString(style.Subtle(" ready in ") + startupDuration(elapsed))
	_, _ = line.WriteString(style.Subtle(" with "))
	_, _ = line.WriteString(style.Normal(fmt.Sprint(info.ProjectSkills + info.GlobalSkills)))
	_, _ = line.WriteString(style.Subtle(" skills, "))
	_, _ = line.WriteString(style.Normal(fmt.Sprint(info.Snippets)))
	_, _ = line.WriteString(style.Subtle(" snippets, and "))
	_, _ = line.WriteString(startupContextTokens(info))
	_, _ = line.WriteString(style.Subtle(" of context."))

	return line.String()
}

func startupDuration(elapsed time.Duration) string {
	var field startupLine
	field.quantity(timeTaken(elapsed), false)
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

func timeTaken(elapsed time.Duration) string {
	if elapsed < time.Millisecond {
		return elapsed.Round(time.Microsecond).String()
	}

	return elapsed.Round(time.Millisecond).String()
}
