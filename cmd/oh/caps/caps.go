// Package caps is the set of capabilities the harness grants.
package caps

import (
	"fmt"
	"strings"
	"sync"

	"crdx.org/io/internal/file"
)

// Set is the set of capabilities granted at one moment, held as one bit each.
type Set uint8

// The capabilities, from reading, which is always granted, to the four that can be switched.
const (
	Read Set = 1 << iota
	Shell
	Write
	Git
	Background
)

const switchable = Shell | Write | Git | Background

var capsMap = []struct {
	grantedCaps Set
	flag        string
}{
	{Read, "r"},
	{Shell, "x"},
	{Write, "w"},
	{Git, "g"},
	{Background, "b"},
}

// AllFlags is every flag Parse accepts, in the order Flags writes them.
var AllFlags = All().Flags()

// All is every capability granted at once.
func All() Set {
	var allCaps Set

	for _, cap := range capsMap {
		allCaps |= cap.grantedCaps
	}

	return allCaps
}

// Flags returns the caps in the form Parse reads.
func (self Set) Flags() string {
	var out strings.Builder

	for _, cap := range capsMap {
		if self.Has(cap.grantedCaps) {
			out.WriteString(cap.flag)
		}
	}

	return out.String()
}

// Has says whether every wanted capability is granted.
func (self Set) Has(want Set) bool { return self&want == want }

// Flag is the single letter for one capability, and nothing for any other combination.
func (self Set) Flag() string {
	for _, cap := range capsMap {
		if cap.grantedCaps == self {
			return cap.flag
		}
	}

	return ""
}

// Parse converts flags into real things.
func Parse(flags string) (Set, error) {
	grantedCaps := Read

	for _, flag := range flags {
		knownCap, found := namedCap(string(flag))
		if !found {
			return 0, fmt.Errorf(
				"unknown capability flag %q — must be one of %q",
				string(flag),
				AllFlags,
			)
		}

		grantedCaps |= knownCap
	}

	return grantedCaps, nil
}

func namedCap(flag string) (Set, bool) {
	for _, knownCap := range capsMap {
		if knownCap.flag == flag {
			return knownCap.grantedCaps, true
		}
	}

	return 0, false
}

// RefuseWrite is the rule the file layer asks before every write, answering from the mode as it
// stands at that moment rather than as it stood when the conversation opened.
func RefuseWrite(mode *Mode) func(name string) error {
	return func(name string) error {
		currentCaps := mode.Current()

		if file.InGitDir(name) {
			if currentCaps.Has(Git) {
				return nil
			}

			return file.ErrGitDir
		}

		if currentCaps.Has(Write) {
			return nil
		}

		return file.ErrReadOnly
	}
}

// Mode tracks current and model-known caps across keypress and turn goroutines.
type Mode struct {
	mutex       sync.Mutex
	currentCaps Set
	knownCaps   Set // what the model thinks is granted (may be outdated now)
}

// NewMode opens a conversation.
func NewMode(currentCaps Set) *Mode {
	return &Mode{currentCaps: currentCaps, knownCaps: currentCaps}
}

// NewResumedMode starts with no caps reported to the model.
func NewResumedMode(currentCaps Set) *Mode {
	return &Mode{currentCaps: currentCaps}
}

// Current is what is granted.
func (self *Mode) Current() Set {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return self.currentCaps
}

// Toggle swaps one capability.
func (self *Mode) Toggle(whichCaps Set) {
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
		changedCaps = switchable
	}

	self.knownCaps = self.currentCaps

	var clauses []string

	if changedCaps.Has(Write) {
		clauses = append(clauses, workspaceIs(self.currentCaps.Has(Write)))
	}

	if changedCaps.Has(Shell) {
		clauses = append(clauses, shellIs(self.currentCaps.Has(Shell)))
	}

	if changedCaps.Has(Git) {
		clauses = append(clauses, historyIs(self.currentCaps.Has(Git)))
	}

	if changedCaps.Has(Background) {
		clauses = append(clauses, backgroundIs(self.currentCaps.Has(Background)))
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
