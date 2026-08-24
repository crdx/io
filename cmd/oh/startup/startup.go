package startup

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/prompt"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/internal/util"
)

var startedAt = time.Now()

func Elapsed() time.Duration {
	return time.Since(startedAt)
}

type Info struct {
	Session       string `json:"session,omitempty"`
	ContextFiles  []File `json:"context,omitempty"`
	ProjectSkills int    `json:"project_skills,omitempty"`
	GlobalSkills  int    `json:"global_skills,omitempty"`
	Snippets      int    `json:"snippets,omitempty"`
	ToolBytes     int    `json:"tools,omitempty"`
}

type File struct {
	Name  string `json:"name"`
	Bytes int    `json:"bytes"`
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

	return style.Subtle("[") + RenderBanner(event.Took, false, info) + style.Subtle("]")
}

// FilesOf reduces prompt context files to the facts shown at startup.
func FilesOf(files []prompt.File) []File {
	kept := make([]File, 0, len(files))

	for _, file := range files {
		kept = append(kept, File{Name: file.Name, Bytes: len(file.Body)})
	}

	return kept
}

// RenderBanner renders the compact startup summary shown outside event replay.
func RenderBanner(elapsed time.Duration, resumed bool, info Info) string {
	if resumed {
		return ""
	}

	var line strings.Builder
	_, _ = line.WriteString(startupDuration(elapsed))
	if info.Session != "" {
		_, _ = line.WriteString(style.Subtle(" session=") + style.Normal(info.Session))
	}
	for _, file := range info.ContextFiles {
		_, _ = line.WriteString(style.Subtle(" ") + startupContextFile(file))
	}
	_, _ = line.WriteString(style.Subtle(" ") + startupSkills(info))
	if info.Snippets > 0 {
		_, _ = line.WriteString(style.Subtle(" ") + startupSnippets(info))
	}
	_, _ = line.WriteString(style.Subtle(" ") + startupTools(info))
	return line.String()
}

func startupDuration(elapsed time.Duration) string {
	var field startupLine
	field.dim("startup=")
	field.quantity(timeTaken(elapsed), false)
	return field.String()
}

func startupContextFile(file File) string {
	var field startupLine
	field.dim(file.Name + "=")
	field.quantity(util.FormatTokenEstimate(file.Bytes, 3), false)
	return field.String()
}

func startupSkills(info Info) string {
	var field startupLine
	field.dim("skills=")
	field.normal(fmt.Sprint(info.ProjectSkills))
	field.dim("p/")
	field.normal(fmt.Sprint(info.GlobalSkills))
	field.dim("g")
	return field.String()
}

func startupSnippets(info Info) string {
	var field startupLine
	field.dim("snippets=")
	field.normal(fmt.Sprint(info.Snippets))
	return field.String()
}

func startupTools(info Info) string {
	var field startupLine
	field.dim("tools=")
	field.quantity(util.FormatTokenEstimate(info.ToolBytes, 2), false)
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
