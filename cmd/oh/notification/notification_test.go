package notification_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/io/cmd/oh/notification"
	"crdx.org/io/cmd/oh/work"
)

func TestTurnErrorNotificationNamesTheWorkspaceAndShowsTheFailure(t *testing.T) {
	bin := t.TempDir()
	capturePath := filepath.Join(t.TempDir(), "arguments")
	fixture := "#!/bin/bash\nset -euo pipefail\nprintf '%s\\n' \"$@\" > \"$NOTIFY_CAPTURE\"\n"
	//nolint:gosec // an executable test fixture
	if err := os.WriteFile(filepath.Join(bin, "notify-send"), []byte(fixture), 0o700); err != nil {
		t.Fatalf("could not write fake notify-send: %v", err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("KITTY_WINDOW_ID", "")
	t.Setenv("NOTIFY_CAPTURE", capturePath)

	if err := notification.SendTurnError(
		t.Context(),
		nil,
		work.At("/workspace/io"),
		errors.New("access denied"),
	); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	//nolint:gosec // the path is a test fixture below t.TempDir
	captured, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatalf("could not read captured arguments: %v", err)
	}
	got := strings.Split(strings.TrimSuffix(string(captured), "\n"), "\n")
	want := []string{
		"--icon=dialog-error",
		"--app-name=oh",
		"--",
		"oh — io",
		"access denied",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got arguments %q, want %q", got, want)
	}
}
