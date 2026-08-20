package main

import (
	"fmt"
	"strings"
	"time"

	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/internal/util"
)

var startedAt = time.Now()

type startupInfo struct {
	sessionID     string
	contextFiles  []contextFile
	projectSkills int
	globalSkills  int
	toolBytes     int
}

func renderStartupBanner(elapsed time.Duration, resumed bool, info startupInfo) string {
	if resumed {
		return ""
	}

	var line strings.Builder
	_, _ = line.WriteString(startupDuration(elapsed))
	if info.sessionID != "" {
		_, _ = line.WriteString(style.Subtle(" session=") + style.Normal(info.sessionID))
	}
	for _, file := range info.contextFiles {
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

func startupContextFile(file contextFile) string {
	var field startupLine
	field.dim(file.name + "=")
	field.quantity(util.FormatTokenEstimate(len(file.body), 3), false)
	return field.String()
}

func startupSkills(info startupInfo) string {
	var field startupLine
	field.dim("skills=")
	field.normal(fmt.Sprint(info.projectSkills))
	field.dim("p/")
	field.normal(fmt.Sprint(info.globalSkills))
	field.dim("g")
	return field.String()
}

func startupTools(info startupInfo) string {
	var field startupLine
	field.dim("tools=")
	field.quantity(util.FormatTokenEstimate(info.toolBytes, 2), false)
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

func (self *startupLine) quantity(text string, unitNormal bool) {
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
	if unitNormal {
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
