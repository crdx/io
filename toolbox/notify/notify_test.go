package notify_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"crdx.org/io/toolbox/notify"
)

func TestAvailabilityFollowsTheNotificationCommandOnPath(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	t.Setenv("KITTY_WINDOW_ID", "")
	if notify.IsAvailable() {
		t.Error("expected notify to be unavailable")
	}

	//nolint:gosec // an executable test fixture
	if err := os.WriteFile(filepath.Join(bin, "notify-send"), nil, 0o700); err != nil {
		t.Fatalf("could not write fake notify-send: %v", err)
	}
	if !notify.IsAvailable() {
		t.Error("expected notify to be available")
	}

	t.Setenv("KITTY_WINDOW_ID", "1")
	if notify.IsAvailable() {
		t.Error("expected notify to be unavailable inside Kitty without the kitten")
	}

	//nolint:gosec // an executable test fixture
	if err := os.WriteFile(filepath.Join(bin, "kitten"), nil, 0o700); err != nil {
		t.Fatalf("could not write fake kitten: %v", err)
	}
	if !notify.IsAvailable() {
		t.Error("expected notify to be available inside Kitty with the kitten")
	}
}

func TestNotificationMapsEveryIconForNotifySend(t *testing.T) {
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

	icons := map[string]string{
		"success":  "emblem-default",
		"info":     "dialog-information",
		"warning":  "dialog-warning",
		"error":    "dialog-error",
		"question": "dialog-question",
		"progress": "process-working",
	}
	for icon, desktopIcon := range icons {
		t.Run(icon, func(t *testing.T) {
			arguments := fmt.Sprintf(
				`{"title":"Build","message":"The build is finished","icon":%q}`,
				icon,
			)
			call, err := notify.New().Parse(arguments)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			result, err := call.Exec(t.Context())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Output != "notified the user" {
				t.Errorf("got output %q, want notification confirmation", result.Output)
			}

			//nolint:gosec // the path is a test fixture below t.TempDir
			captured, err := os.ReadFile(capturePath)
			if err != nil {
				t.Fatalf("could not read captured arguments: %v", err)
			}
			got := strings.Split(strings.TrimSuffix(string(captured), "\n"), "\n")
			want := []string{
				"--icon=" + desktopIcon,
				"--app-name=oh",
				"Build",
				"The build is finished",
			}
			if !slices.Equal(got, want) {
				t.Errorf("got arguments %q, want %q", got, want)
			}
		})
	}
}

func TestNotificationUsesKittysNotificationKittenInsideKitty(t *testing.T) {
	bin := t.TempDir()
	capturePath := filepath.Join(t.TempDir(), "arguments")
	fixture := "#!/bin/bash\nset -euo pipefail\nprintf '%s\\n' \"$@\" > \"$NOTIFY_CAPTURE\"\n"
	//nolint:gosec // an executable test fixture
	if err := os.WriteFile(filepath.Join(bin, "kitten"), []byte(fixture), 0o700); err != nil {
		t.Fatalf("could not write fake kitten: %v", err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("KITTY_WINDOW_ID", "1")
	t.Setenv("NOTIFY_CAPTURE", capturePath)

	call, err := notify.New().Parse(`{"title":"Build","message":"The build is finished","icon":"error"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := call.Exec(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "notified the user" {
		t.Errorf("got output %q, want notification confirmation", result.Output)
	}

	//nolint:gosec // the path is a test fixture below t.TempDir
	captured, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatalf("could not read captured arguments: %v", err)
	}
	got := strings.Split(strings.TrimSuffix(string(captured), "\n"), "\n")
	want := []string{
		"notify",
		"--icon=dialog-error",
		"--app-name=oh",
		"Build",
		"The build is finished",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got arguments %q, want %q", got, want)
	}
}

func TestNotificationTitleIsRequired(t *testing.T) {
	if _, err := notify.New().Parse(`{"title":"  ","message":"hello","icon":"info"}`); err == nil {
		t.Error("expected a blank title to be refused")
	}
}

func TestNotificationMessageIsRequired(t *testing.T) {
	if _, err := notify.New().Parse(`{"title":"Greeting","message":"  ","icon":"info"}`); err == nil {
		t.Error("expected a blank message to be refused")
	}
}

func TestNotificationIconIsConstrained(t *testing.T) {
	for _, icon := range []string{"", "good", "dialog-warning"} {
		arguments := fmt.Sprintf(`{"title":"Greeting","message":"hello","icon":%q}`, icon)
		if _, err := notify.New().Parse(arguments); err == nil {
			t.Errorf("expected icon %q to be refused", icon)
		} else if !strings.Contains(err.Error(), "success, info, warning, error, question, progress") {
			t.Errorf("expected every choice in the error, got %q", err)
		}
	}
}

func TestNotificationReportsNotifySendFailure(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("KITTY_WINDOW_ID", "")

	call, err := notify.New().Parse(`{"title":"Greeting","message":"hello","icon":"info"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = call.Exec(t.Context())
	if err == nil || !strings.Contains(err.Error(), "could not notify the user") {
		t.Errorf("expected notify-send failure, got %v", err)
	}
}

func TestCancelledNotificationStopsNotifySend(t *testing.T) {
	bin := t.TempDir()
	fixture := "#!/bin/bash\nset -euo pipefail\nexec /bin/sleep 60\n"
	//nolint:gosec // an executable test fixture
	if err := os.WriteFile(filepath.Join(bin, "notify-send"), []byte(fixture), 0o700); err != nil {
		t.Fatalf("could not write fake notify-send: %v", err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("KITTY_WINDOW_ID", "")

	call, err := notify.New().Parse(`{"title":"Greeting","message":"hello","icon":"info"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(100*time.Millisecond, cancel)

	startedAt := time.Now()
	_, err = call.Exec(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context cancellation, got %v", err)
	}
	if took := time.Since(startedAt); took > 2*time.Second {
		t.Errorf("expected notify-send to stop promptly, took %s", took)
	}
}

func TestIconParameterDescriptionListsEveryChoice(t *testing.T) {
	var description string
	for _, parameter := range notify.New().Schema() {
		if parameter.Name == "icon" {
			description = parameter.Description
		}
	}

	for _, icon := range []string{"success", "info", "warning", "error", "question", "progress"} {
		if !strings.Contains(description, icon) {
			t.Errorf("icon description %q does not list %q", description, icon)
		}
	}
}

func TestDescribeReportsTheTitleAndMessage(t *testing.T) {
	subject, qualifier := notify.Describe(notify.Args{Title: "Greeting", Message: "hello"})
	if subject != "Greeting" || qualifier != "hello" {
		t.Errorf("got subject %q and qualifier %q", subject, qualifier)
	}
}
