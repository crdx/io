package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const watchTestTimeout = 2 * time.Second

func observeConfig(t *testing.T, path string) (Config, *Observer) {
	t.Helper()

	settings, observer, err := Observe(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(observer.Close)
	return settings, observer
}

func awaitObservedConfig(t *testing.T, observer *Observer) (Config, error) {
	t.Helper()

	timeout := time.NewTimer(watchTestTimeout)
	defer timeout.Stop()
	for {
		select {
		case failure, open := <-observer.Changes():
			if !open {
				t.Fatal("config watch closed before reporting the change")
			}
			settings, changed, err := observer.refresh(failure)
			if err != nil || changed {
				return settings, err
			}
		case <-timeout.C:
			t.Fatal("timed out waiting for a config change")
		}
	}
}

func TestWritingAnObservedConfigLoadsTheNewRevision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "get_on_with_it_message = \"first\"\n"); err != nil {
		t.Fatal(err)
	}

	settings, observer := observeConfig(t, path)
	if settings.GetOnWithItMessage != "first" {
		t.Errorf("got initial message %q", settings.GetOnWithItMessage)
	}
	if err := writeConfigFile(path, "get_on_with_it_message = \"second\"\n"); err != nil {
		t.Fatal(err)
	}

	settings, err := awaitObservedConfig(t, observer)
	if err != nil {
		t.Fatal(err)
	}
	if settings.GetOnWithItMessage != "second" {
		t.Errorf("got changed message %q", settings.GetOnWithItMessage)
	}
}

func TestAtomicallyReplacingAnObservedConfigLoadsTheNewRevision(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.toml")
	if err := writeConfigFile(path, "get_on_with_it_message = \"first\"\n"); err != nil {
		t.Fatal(err)
	}
	_, observer := observeConfig(t, path)

	replacement := filepath.Join(directory, "replacement.toml")
	if err := writeConfigFile(replacement, "get_on_with_it_message = \"replacement\"\n"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}

	settings, err := awaitObservedConfig(t, observer)
	if err != nil {
		t.Fatal(err)
	}
	if settings.GetOnWithItMessage != "replacement" {
		t.Errorf("got replacement message %q", settings.GetOnWithItMessage)
	}
}

func TestCreatingAConfigBelowMissingDirectoriesReplacesTheDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "one", "two", "config.toml")
	settings, observer := observeConfig(t, path)
	if settings.GetOnWithItMessage != "yes" {
		t.Errorf("initial default message=%q", settings.GetOnWithItMessage)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeConfigFile(path, "get_on_with_it_message = \"created\"\n"); err != nil {
		t.Fatal(err)
	}

	settings, err := awaitObservedConfig(t, observer)
	if err != nil {
		t.Fatal(err)
	}
	if settings.GetOnWithItMessage != "created" {
		t.Errorf("got created message %q", settings.GetOnWithItMessage)
	}
}

func TestDeletingAnObservedConfigRestoresTheDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "get_on_with_it_message = \"configured\"\n"); err != nil {
		t.Fatal(err)
	}
	_, observer := observeConfig(t, path)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	settings, err := awaitObservedConfig(t, observer)
	if err != nil {
		t.Fatal(err)
	}
	if settings.GetOnWithItMessage != "yes" {
		t.Errorf("got default message %q", settings.GetOnWithItMessage)
	}
}

func TestAnInvalidObservedRevisionIsReportedOnlyOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "get_on_with_it_message = \"first\"\n"); err != nil {
		t.Fatal(err)
	}
	_, observer := observeConfig(t, path)
	if err := os.WriteFile(path, []byte("not toml = ["), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := awaitObservedConfig(t, observer); err == nil {
		t.Fatal("expected the invalid revision to fail")
	}
	if _, changed, err := observer.refresh(nil); err != nil || changed {
		t.Errorf("repeated revision changed=%t err=%v", changed, err)
	}
}

func TestAnUnrelatedDirectoryEventDoesNotChangeTheObservedConfig(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.toml")
	if err := writeConfigFile(path, "get_on_with_it_message = \"kept\"\n"); err != nil {
		t.Fatal(err)
	}
	_, observer := observeConfig(t, path)
	if err := os.WriteFile(filepath.Join(directory, "other"), []byte("other"), 0o600); err != nil {
		t.Fatal(err)
	}

	select {
	case failure := <-observer.Changes():
		if _, changed, err := observer.refresh(failure); err != nil || changed {
			t.Errorf("unrelated event changed=%t err=%v", changed, err)
		}
	case <-time.After(watchTestTimeout):
		t.Fatal("timed out waiting for the directory event")
	}
}

func TestAValidReloadAfterAFailureIsApplied(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "get_on_with_it_message = \"first\"\n"); err != nil {
		t.Fatal(err)
	}
	observer := &Observer{path: path, handled: readSnapshot(path)}

	failed := observer.Reload(errors.New("watch stopped"), testSegments())
	if failed.Status != ReloadFailed || failed.Failure == nil {
		t.Fatalf("failed reload status=%v failure=%v", failed.Status, failed.Failure)
	}
	if err := writeConfigFile(path, "get_on_with_it_message = \"recovered\"\n"); err != nil {
		t.Fatal(err)
	}

	applied := observer.Reload(nil, testSegments())
	if applied.Status != ReloadApplied || applied.Failure != nil {
		t.Fatalf("applied reload status=%v failure=%v", applied.Status, applied.Failure)
	}
	if applied.LiveConfig.GetOnWithItMessage != "recovered" {
		t.Errorf("applied message=%q", applied.LiveConfig.GetOnWithItMessage)
	}
}

func TestObserveReportsAnInvalidInitialConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("not toml = ["), 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, err := Observe(path)
	if err == nil || !strings.Contains(err.Error(), "config.toml") {
		t.Errorf("got %v", err)
	}
}

func TestClosingAnObserverClosesItsChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "get_on_with_it_message = \"first\"\n"); err != nil {
		t.Fatal(err)
	}
	_, observer, err := Observe(path)
	if err != nil {
		t.Fatal(err)
	}
	observer.Close()

	if _, open := <-observer.Changes(); open {
		t.Error("changes remained open")
	}
}
