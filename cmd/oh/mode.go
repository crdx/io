package main

import (
	"fmt"
	"strings"
	"sync"

	"crdx.org/io/internal/file"
)

type caps uint8

const (
	capRead       caps = 1 << iota // reading, which is granted whatever is asked for
	capShell                       // running a shell command
	capWrite                       // changing files in the workspace
	capGit                         // changing .git
	capBackground                  // letting a shell command leave processes behind
)

const switchableCaps = capShell | capWrite | capGit | capBackground // everything but reading

var capsMap = []struct {
	grantedCaps caps
	flag        string
}{
	{capRead, "r"},
	{capShell, "x"},
	{capWrite, "w"},
	{capGit, "g"},
	{capBackground, "b"},
}

var capFlags = allCaps().Flags() // as --caps and the rule over the input spell them

func allCaps() caps {
	var allCaps caps

	for _, cap := range capsMap {
		allCaps |= cap.grantedCaps
	}

	return allCaps
}

// Flags returns caps in the form Caps reads.
func (self caps) Flags() string {
	var out strings.Builder

	for _, cap := range capsMap {
		if self.has(cap.grantedCaps) {
			out.WriteString(cap.flag)
		}
	}

	return out.String()
}

func (self caps) has(want caps) bool { return self&want == want }

func (self caps) flag() string {
	for _, cap := range capsMap {
		if cap.grantedCaps == self {
			return cap.flag
		}
	}

	return ""
}

// Caps converts flags into real things.
func Caps(flags string) (caps, error) {
	grantedCaps := capRead

	for _, flag := range flags {
		knownCap, found := namedCap(string(flag))
		if !found {
			return 0, fmt.Errorf(
				"unknown capability flag %q — must be one of %q",
				string(flag),
				capFlags,
			)
		}

		grantedCaps |= knownCap
	}

	return grantedCaps, nil
}

func namedCap(flag string) (caps, bool) {
	for _, knownCap := range capsMap {
		if knownCap.flag == flag {
			return knownCap.grantedCaps, true
		}
	}

	return 0, false
}

func refuseWrite(mode *Mode) func(name string) error {
	return func(name string) error {
		currentCaps := mode.Current()

		if file.InGitDir(name) {
			if currentCaps.has(capGit) {
				return nil
			}

			return file.ErrGitDir
		}

		if currentCaps.has(capWrite) {
			return nil
		}

		return file.ErrReadOnly
	}
}

// Mode tracks current and model-known caps across keypress and turn goroutines.
type Mode struct {
	mutex       sync.Mutex
	currentCaps caps
	knownCaps   caps // what the model thinks is granted (may be outdated now)
}

// NewMode opens a conversation.
func NewMode(currentCaps caps) *Mode {
	return &Mode{currentCaps: currentCaps, knownCaps: currentCaps}
}

// NewResumedMode starts with no caps reported to the model.
func NewResumedMode(currentCaps caps) *Mode {
	return &Mode{currentCaps: currentCaps}
}

// Current is what is granted.
func (self *Mode) Current() caps {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return self.currentCaps
}

// Toggle swaps one capability.
func (self *Mode) Toggle(whichCaps caps) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.currentCaps ^= whichCaps
}

// Inject is what to tell the model about its changed environment before the next message.
func (self *Mode) Inject() string {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	changedCaps := self.currentCaps ^ self.knownCaps
	if self.knownCaps == 0 {
		changedCaps = switchableCaps
	}

	self.knownCaps = self.currentCaps

	var clauses []string

	if changedCaps.has(capWrite) {
		clauses = append(clauses, workspaceIs(self.currentCaps.has(capWrite)))
	}

	if changedCaps.has(capShell) {
		clauses = append(clauses, shellIs(self.currentCaps.has(capShell)))
	}

	if changedCaps.has(capGit) {
		clauses = append(clauses, historyIs(self.currentCaps.has(capGit)))
	}

	if changedCaps.has(capBackground) {
		clauses = append(clauses, backgroundIs(self.currentCaps.has(capBackground)))
	}

	return strings.Join(clauses, " ")
}

func workspaceIs(writable bool) string {
	if writable {
		return "The workspace is now read-write."
	}

	return "The workspace is now read-only."
}

func shellIs(granted bool) string {
	if granted {
		return "The bash tool can now run shell commands."
	}

	return "The bash tool is now refused, and will turn away every command until it is granted again."
}

func historyIs(writable bool) string {
	if writable {
		return "The .git directory is now read-write."
	}

	return "The .git directory is now read-only."
}

func backgroundIs(enabled bool) string {
	if enabled {
		return "Background processes can now outlive shell commands."
	}

	return "Background processes have been killed and new ones will no longer outlive shell commands."
}
