package sandbox

import (
	"strings"
	"testing"
)

func TestWhatCrossesToTheKeeperMustSurviveTheCrossing(t *testing.T) {
	for _, probe := range []struct {
		name      string
		directory string
		command   string
		wanted    string
	}{
		{name: "a null byte in the directory", directory: "/work\x00ing", command: "true", wanted: "null byte"},
		{name: "a null byte in the command", directory: "/work", command: "true\x00", wanted: "null byte"},
		{name: "invalid UTF-8 in the directory", directory: "/work/\xffodd", command: "true", wanted: "UTF-8"},
		{name: "invalid UTF-8 in the command", directory: "/work", command: "ls /\xffodd", wanted: "UTF-8"},
	} {
		t.Run(probe.name, func(t *testing.T) {
			err := ensureSane(probe.directory, probe.command)
			if err == nil {
				t.Fatal("it was accepted, and would have been silently rewritten")
			}
			if !strings.Contains(err.Error(), probe.wanted) {
				t.Errorf("got %v, want it to name the %s", err, probe.wanted)
			}
		})
	}
}

func TestOrdinaryCommandsCrossUnchanged(t *testing.T) {
	if err := ensureSane("/workspace/project", "echo hello \u2713"); err != nil {
		t.Errorf("an ordinary command was refused: %v", err)
	}
}
