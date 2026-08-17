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
	capWrite                       // changing files in the workspace
	capShell                       // running a shell command
	capGit                         // changing .git
	capBackground                  // letting a shell command leave processes behind
)

const switchableCaps = capWrite | capShell | capGit | capBackground // everything but reading

var capMap = []struct {
	grantedCaps caps
	symbol      string
}{
	{capRead, "r"},
	{capWrite, "w"},
	{capShell, "x"},
	{capGit, "g"},
	{capBackground, "b"},
}

var capLetters = allCapabilities().Letters() // as --caps and the rule over the input spell them

func allCapabilities() caps {
	var allCaps caps

	for _, cap := range capMap {
		allCaps |= cap.grantedCaps
	}

	return allCaps
}

// Letters returns capabilities in the form Caps reads.
func (self caps) Letters() string {
	var out strings.Builder

	for _, cap := range capMap {
		if self.has(cap.grantedCaps) {
			out.WriteString(cap.symbol)
		}
	}

	return out.String()
}

func (self caps) has(want caps) bool { return self&want == want }

func (self caps) letter() string {
	for _, cap := range capMap {
		if cap.grantedCaps == self {
			return cap.symbol
		}
	}

	return ""
}

// Caps reads capabilities as they are spelled. Reading is granted whether it was asked for or not,
// an assistant that cannot read a file being no assistant, so r is there for the look of the
// thing, and a letter naming nothing is refused rather than passed over.
func Caps(spelled string) (caps, error) {
	grantedCaps := capRead

	for _, letter := range spelled {
		knownCap, found := namedCapability(string(letter))
		if !found {
			return 0, fmt.Errorf(
				"a capability is spelled with one of the letters %s, and %q is none of them",
				capLetters, string(letter),
			)
		}

		grantedCaps |= knownCap
	}

	return grantedCaps, nil
}

func namedCapability(letter string) (caps, bool) {
	for _, knownCap := range capMap {
		if knownCap.symbol == letter {
			return knownCap.grantedCaps, true
		}
	}

	return 0, false
}

func refuseWrite(mode *Mode) func(name string) error {
	return func(name string) error {
		currentCaps := mode.Current() // asked afresh, the person at the keyboard having a say between calls

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

// Mode tracks current and model-known capabilities across keypress and turn goroutines.
type Mode struct {
	mutex       sync.Mutex
	currentCaps caps // what is granted
	knownCaps   caps // what the model thinks is granted (may be outdated now)
}

// NewMode opens a conversation.
func NewMode(currentCaps caps) *Mode {
	return &Mode{currentCaps: currentCaps, knownCaps: currentCaps}
}

// NewResumedMode starts with no capabilities reported to the model.
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
	if self.knownCaps == 0 { // told nothing so far, so told the state of everything rather than a change
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
