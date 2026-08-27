package caps

import (
	"fmt"
	"strings"
	"sync"

	"crdx.org/io/internal/file"
)

type Set uint8

const (
	Read Set = 1 << iota
	Shell
	Write
	Git
	Web
)

var capsMap = []struct {
	grantedCaps Set
	flag        string
}{
	{Read, "r"},
	{Shell, "x"},
	{Write, "w"},
	{Git, "g"},
	{Web, "s"},
}

var AllFlags = All().Flags()

func All() Set {
	var allCaps Set

	for _, cap := range capsMap {
		allCaps |= cap.grantedCaps
	}

	return allCaps
}

func (self Set) Flags() string {
	var out strings.Builder

	for _, cap := range capsMap {
		if self.Has(cap.grantedCaps) {
			out.WriteString(cap.flag)
		}
	}

	return out.String()
}

func (self Set) Has(want Set) bool { return self&want == want }

func (self Set) Flag() string {
	for _, cap := range capsMap {
		if cap.grantedCaps == self {
			return cap.flag
		}
	}

	return ""
}

func Named(flag string) (Set, bool) {
	for _, knownCap := range capsMap {
		if knownCap.flag == flag {
			return knownCap.grantedCaps, true
		}
	}

	return 0, false
}

func Parse(flags string) (Set, error) {
	grantedCaps := Read

	for _, flag := range flags {
		knownCap, found := Named(string(flag))
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

type Mode struct {
	mutex           sync.Mutex
	currentCaps     Set
	knownCaps       Set
	unconfirmedCaps Set
	owedCaps        Set
}

func NewMode(currentCaps Set) *Mode {
	return &Mode{currentCaps: currentCaps, knownCaps: currentCaps}
}

func NewResumedMode(currentCaps Set) *Mode {
	return &Mode{currentCaps: currentCaps}
}

func (self *Mode) Current() Set {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return self.currentCaps
}

func (self *Mode) Toggle(whichCaps Set) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.currentCaps ^= whichCaps
}

func (self *Mode) Inject() string {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	changedCaps := (self.currentCaps ^ self.knownCaps) | self.owedCaps
	if self.knownCaps == 0 {
		changedCaps = All()
	}

	self.knownCaps = self.currentCaps
	self.unconfirmedCaps |= changedCaps
	self.owedCaps = 0

	return lexicalDiff(changedCaps, self.currentCaps)
}

func (self *Mode) Acknowledge() {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.unconfirmedCaps = 0
}

func (self *Mode) Retract() {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.owedCaps = self.unconfirmedCaps
}

func lexicalDiff(changedCaps Set, currentCaps Set) string {
	var clauses []string

	if changedCaps.Has(Write) {
		clauses = append(clauses, workspaceIs(currentCaps.Has(Write)))
	}
	if changedCaps.Has(Shell) {
		clauses = append(clauses, shellIs(currentCaps.Has(Shell)))
	}
	if changedCaps.Has(Git) {
		clauses = append(clauses, historyIs(currentCaps.Has(Git)))
	}
	if changedCaps.Has(Web) {
		clauses = append(clauses, webIs(currentCaps.Has(Web)))
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

func webIs(granted bool) string {
	if granted {
		return "The web search and fetch tools can now access the internet."
	}

	return "The web search and fetch tools are now refused."
}
