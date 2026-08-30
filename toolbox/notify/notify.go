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

type EscapeWriter func(escape string) bool

func IsAvailable() bool {
	if isKitty() {
		_, err := exec.LookPath("kitten")
		return err == nil
	}

	_, err := exec.LookPath("notify-send")
	return err == nil
}

func Command(ctx context.Context, title string, message string, icon string) (*exec.Cmd, bool) {
	if isKitty() {
		//nolint:gosec // the executable and options are fixed, and the arguments are inert
		return exec.CommandContext(ctx, "kitten", "notify", "--only-print-escape-code", "--icon="+icon, "--app-name="+applicationName, title, message), true
	}

	//nolint:gosec // the executable and options are fixed, and the arguments are inert
	return exec.CommandContext(ctx, "notify-send", "--icon="+icon, "--app-name="+applicationName, title, message), false
}

func isKitty() bool {
	return os.Getenv("KITTY_WINDOW_ID") != ""
}

type Args struct {
	Title   string `json:"title"`
	Message string `json:"message"`
	Icon    string `json:"icon"`
}

func New(writeEscape EscapeWriter) tool.Tool {
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
		Plain(func(ctx context.Context, args Args) (string, error) {
			return run(ctx, writeEscape, args)
		})
}

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

func run(ctx context.Context, writeEscape EscapeWriter, args Args) (string, error) {
	command, printsEscapeCode := Command(ctx, args.Title, args.Message, desktopIconNames[args.Icon])

	var escape strings.Builder
	if printsEscapeCode {
		if writeEscape == nil {
			return "", errors.New("could not notify the user: nothing to write the notification to")
		}

		command.Stdout = &escape
	}

	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return "", stop.Error(ctx, "the notification")
		}

		return "", fmt.Errorf("could not notify the user: %w", err)
	}

	if printsEscapeCode && !writeEscape(escape.String()) {
		return "", errors.New("could not notify the user: the terminal that raises it is not there")
	}

	return "notified the user", nil
}
