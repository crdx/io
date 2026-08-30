package dispatch_test

import (
	"os"
	"path/filepath"
	"testing"

	"crdx.org/io/cmd/oh/dispatch"
	"crdx.org/io/cmd/oh/slash"
)

func TestHandleTreatsAValidPathAsAnOrdinaryMessage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "valid path")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}

	registry := newTestRegistry(t)
	result, failure := dispatch.Handle(registry, dispatch.Actions{}, path)
	if result != dispatch.Ordinary || failure != "" {
		t.Errorf("got result %d and failure %q", result, failure)
	}
}

func TestHandleRejectsExistingPathsWithFewerThanTwoParts(t *testing.T) {
	registry := newTestRegistry(t)
	for _, path := range []string{"/", "/home"} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}

		result, failure := dispatch.Handle(registry, dispatch.Actions{}, path)
		wantFailure := "Command not found: " + path + " (alt+enter sends as message)"
		if result != dispatch.Rejected || failure != wantFailure {
			t.Errorf("Handle(%q) got result %d and failure %q, want %q", path, result, failure, wantFailure)
		}
	}
}

func newTestRegistry(t *testing.T) slash.Registry {
	t.Helper()

	set, err := slash.NewCommandSet("/")
	if err != nil {
		t.Fatal(err)
	}
	registry, err := slash.NewRegistry(set)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}
