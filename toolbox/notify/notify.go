package notify

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"crdx.org/io/internal/stop"
	"crdx.org/io/tool"
)

const (
	applicationName = "oh"
	iconChoices     = "success, info, warning, error, question, progress"
)

var desktopIconNames = map[string]string{
	"success":  "emblem-default",
	"info":     "dialog-information",
	"warning":  "dialog-warning",
	"error":    "dialog-error",
	"question": "dialog-question",
	"progress": "process-working",
}

// IsAvailable reports whether a notification can be shown: with Kitty's notification kitten when
// running inside Kitty, and with notify-send elsewhere.
func IsAvailable() bool {
	if isKitty() {
		_, err := exec.LookPath("kitten")
		return err == nil
	}

	_, err := exec.LookPath("notify-send")
	return err == nil
}

// Command builds the command that shows a desktop notification, preferring Kitty's notification
// kitten when running inside Kitty so that clicking the notification focuses the terminal. The
// second result reports whether the command writes a Kitty escape code to the terminal rather than
// talking directly to the notification service, in which case its standard output must be the
// terminal.
func Command(ctx context.Context, title, message, icon string) (*exec.Cmd, bool) {
	if isKitty() {
		//nolint:gosec // the executable and options are fixed, and the arguments are inert
		return exec.CommandContext(ctx, "kitten", "notify", "--icon="+icon, "--app-name="+applicationName, title, message), true
	}

	//nolint:gosec // the executable and options are fixed, and the arguments are inert
	return exec.CommandContext(ctx, "notify-send", "--icon="+icon, "--app-name="+applicationName, title, message), false
}

func isKitty() bool {
	return os.Getenv("KITTY_WINDOW_ID") != ""
}

// Args is what a desktop notification takes.
type Args struct {
	Title   string `json:"title"`
	Message string `json:"message"`
	Icon    string `json:"icon"`
}

// New builds a tool that alerts the user with a desktop notification.
func New() tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name:        "notify",
			Description: "send a desktop notification to alert the user",
			Schema: tool.Schema{
				tool.String("title", "notification title"),
				tool.String("message", "notification text"),
				tool.String("icon", "notification icon: one of "+iconChoices),
			},
		},
		Describe,
	).
		Validate(validate).
		Plain(run)
}

// Describe reports the notification title and text.
func Describe(args Args) (string, string) {
	return args.Title, args.Message
}

func validate(args Args) error {
	if strings.TrimSpace(args.Title) == "" {
		return errors.New("title is required")
	}
	if strings.TrimSpace(args.Message) == "" {
		return errors.New("message is required")
	}
	if _, isKnown := desktopIconNames[args.Icon]; !isKnown {
		return fmt.Errorf("icon must be one of: %s", iconChoices)
	}

	return nil
}

func run(ctx context.Context, args Args) (string, error) {
	command, writesToTerminal := Command(ctx, args.Title, args.Message, desktopIconNames[args.Icon])
	if writesToTerminal {
		command.Stdout = os.Stdout
	}

	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return "", stop.Error(ctx, "the notification")
		}

		return "", fmt.Errorf("could not notify the user: %w", err)
	}

	return "notified the user", nil
}
