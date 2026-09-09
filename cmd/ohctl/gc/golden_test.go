package gc

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/cmd/ohctl/console"
	"crdx.org/io/session"
)

const goldenName = "stored-session"

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

func TestUsageMatchesTheGolden(t *testing.T) {
	assertGolden(t, "usage.txt", strings.ReplaceAll(usage, "$0", "ohctl"))
}

func TestEveryCacheIsRemovedAndReported(t *testing.T) {
	directories, name := populated(t)

	var screen, failure strings.Builder
	if err := run(directories, options{}, console.Output{Screen: &screen, Failure: &failure}); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "removed.txt", report(screen.String(), failure.String(), name))
	assertGone(t, filepath.Join(directories.Farm, name, ".cache"))
	assertGone(t, filepath.Join(directories.Home, ".cache"))

	nested := filepath.Join(directories.Farm, name, "checkout", ".cache")
	if _, err := os.Stat(nested); err != nil {
		t.Errorf("a sweep of the roots took %s", nested)
	}
}

func TestAnAggressiveSweepTakesTheCachesWithinACheckout(t *testing.T) {
	directories, name := populated(t)

	var screen, failure strings.Builder
	choice := options{isAggressive: true}
	if err := run(directories, choice, console.Output{Screen: &screen, Failure: &failure}); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "aggressive.txt", report(screen.String(), failure.String(), name))
	assertGone(t, filepath.Join(directories.Farm, name, "checkout", ".cache"))
}

func TestADryRunReportsWhatItWouldRemoveAndRemovesNothing(t *testing.T) {
	directories, name := populated(t)

	var screen, failure strings.Builder
	if err := run(directories, options{isDryRun: true}, console.Output{Screen: &screen, Failure: &failure}); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "dry-run.txt", report(screen.String(), failure.String(), name))

	kept := filepath.Join(directories.Farm, name, ".cache")
	if _, err := os.Stat(kept); err != nil {
		t.Errorf("a dry run removed %s", kept)
	}
}

func TestARunningSessionKeepsItsCaches(t *testing.T) {
	directories, name := populated(t)

	heldLock, err := session.AcquireLock(directories.Sessions, name)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = heldLock.Release() }()

	var screen, failure strings.Builder
	if err := run(directories, options{}, console.Output{Screen: &screen, Failure: &failure}); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "running.txt", report(screen.String(), failure.String(), name))

	kept := filepath.Join(directories.Farm, name, ".cache")
	if _, err := os.Stat(kept); err != nil {
		t.Errorf("a running session lost %s", kept)
	}
}

func TestAReadOnlyModuleCacheIsStillRemoved(t *testing.T) {
	directories, name := populated(t)

	module := filepath.Join(directories.Farm, name, ".cache", "go-mod", "yaml.v3")
	write(t, filepath.Join(module, "writerc.go"), 128)
	if err := os.Chmod(module, 0o500); err != nil { //nolint:gosec // a read-only cache is the point
		t.Fatal(err)
	}

	var screen, failure strings.Builder
	if err := run(directories, options{}, console.Output{Screen: &screen, Failure: &failure}); err != nil {
		t.Fatal(err)
	}

	assertGone(t, filepath.Join(directories.Farm, name, ".cache"))
}

func TestNothingToRemoveIsStillReported(t *testing.T) {
	directories := Directories{Farm: t.TempDir(), Sessions: t.TempDir(), Home: t.TempDir()}

	var screen, failure strings.Builder
	if err := run(directories, options{}, console.Output{Screen: &screen, Failure: &failure}); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "nothing.txt", report(screen.String(), failure.String(), goldenName))
}

func populated(t *testing.T) (Directories, string) {
	t.Helper()

	directories := Directories{Farm: t.TempDir(), Sessions: t.TempDir(), Home: t.TempDir()}
	name := storedSession(t, directories.Sessions)

	write(t, filepath.Join(directories.Farm, name, ".cache", "npm", "packed"), 4096)
	write(t, filepath.Join(directories.Farm, name, ".cache", "kept-below"), 1024)
	write(t, filepath.Join(directories.Farm, name, "checkout", ".cache", "built"), 2048)
	write(t, filepath.Join(directories.Farm, name, "checkout", "source.go"), 512)
	write(t, filepath.Join(directories.Farm, "able-dolphin", ".cache", "left-behind"), 8192)
	write(t, filepath.Join(directories.Home, ".cache", "go-build", "object"), 16384)
	write(t, filepath.Join(directories.Home, ".config", "settings"), 256)

	return directories, name
}

func storedSession(t *testing.T, directory string) string {
	t.Helper()

	writer, err := store.Create(directory, store.Meta{
		WorkspaceDir: t.TempDir(),
		Model:        "gpt-5.6-sol",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "first question"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	return writer.Name()
}

func write(t *testing.T, path string, bytes int) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, bytes), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertGone(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("%s is still there", path)
	}
}

func report(screen string, failure string, named string) string {
	text := strings.Join([]string{
		"=== screen ===\n", screen,
		"=== failure ===\n", failure,
	}, "")

	return strings.ReplaceAll(text, named, goldenName)
}

func assertGolden(t *testing.T, name string, drawn string) {
	t.Helper()

	goldenPath := filepath.Join("testdata", name)
	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(drawn), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if drawn != string(want) {
		t.Errorf("output differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, drawn, want)
	}
}
