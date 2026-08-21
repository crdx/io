package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/internal/util"
)

var startedAt = time.Now()

type startupInfo struct {
	SessionID     string        `json:"session,omitempty"`
	ContextFiles  []startupFile `json:"context,omitempty"`
	ProjectSkills int           `json:"project_skills,omitempty"`
	GlobalSkills  int           `json:"global_skills,omitempty"`
	ToolBytes     int           `json:"tools,omitempty"`
}

type startupFile struct {
	Name  string `json:"name"`
	Bytes int    `json:"bytes"`
}

func startupEvent(elapsed time.Duration, info startupInfo) agent.Event {
	facts, err := json.Marshal(info)
	if err != nil {
		return agent.Event{Kind: agent.Startup, Took: elapsed}
	}

	return agent.Event{Kind: agent.Startup, Took: elapsed, State: facts}
}

func renderStartupEvent(event agent.Event) string {
	var info startupInfo
	if len(event.State) > 0 {
		if err := json.Unmarshal(event.State, &info); err != nil {
			return ""
		}
	}

	return style.Subtle("[") + renderStartupBanner(event.Took, false, info) + style.Subtle("]")
}

func startupFilesOf(files []contextFile) []startupFile {
	kept := make([]startupFile, 0, len(files))

	for _, file := range files {
		kept = append(kept, startupFile{Name: file.name, Bytes: len(file.body)})
	}

	return kept
}

func renderStartupBanner(elapsed time.Duration, resumed bool, info startupInfo) string {
	if resumed {
		return ""
	}

	var line strings.Builder
	_, _ = line.WriteString(startupDuration(elapsed))
	if info.SessionID != "" {
		_, _ = line.WriteString(style.Subtle(" session=") + style.Normal(info.SessionID))
	}
	for _, file := range info.ContextFiles {
		_, _ = line.WriteString(style.Subtle(" ") + startupContextFile(file))
	}
	_, _ = line.WriteString(style.Subtle(" ") + startupSkills(info))
	_, _ = line.WriteString(style.Subtle(" ") + startupTools(info))
	return line.String()
}

func startupDuration(elapsed time.Duration) string {
	var field startupLine
	field.dim("startup=")
	field.quantity(timeTaken(elapsed), false)
	return field.String()
}

func startupContextFile(file startupFile) string {
	var field startupLine
	field.dim(file.Name + "=")
	field.quantity(util.FormatTokenEstimate(file.Bytes, 3), false)
	return field.String()
}

func startupSkills(info startupInfo) string {
	var field startupLine
	field.dim("skills=")
	field.normal(fmt.Sprint(info.ProjectSkills))
	field.dim("p/")
	field.normal(fmt.Sprint(info.GlobalSkills))
	field.dim("g")
	return field.String()
}

func startupTools(info startupInfo) string {
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
