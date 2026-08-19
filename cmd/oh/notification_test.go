package main

import (
	"slices"
	"testing"
)

func TestTurnFinishedNotificationUsesKittyWhenAvailable(t *testing.T) {
	command, writesToTerminal := newTurnFinishedNotificationCommand("/workspace/io", true)

	want := []string{
		"kitten",
		"notify",
		"--icon=utilities-terminal",
		"--app-name=oh",
		"oh",
		"A model in io is waiting for you",
	}
	if !slices.Equal(command.Args, want) {
		t.Errorf("got command %q, want %q", command.Args, want)
	}
	if !writesToTerminal {
		t.Error("expected the Kitty notification escape code to be written to the terminal")
	}
}

func TestTurnFinishedNotificationFallsBackToNotifySend(t *testing.T) {
	command, writesToTerminal := newTurnFinishedNotificationCommand("/workspace/io", false)

	want := []string{
		"notify-send",
		"--app-name=oh",
		"--icon=utilities-terminal",
		"oh",
		"A model in io is waiting for you",
	}
	if !slices.Equal(command.Args, want) {
		t.Errorf("got command %q, want %q", command.Args, want)
	}
	if writesToTerminal {
		t.Error("expected notify-send to talk directly to the notification service")
	}
}
