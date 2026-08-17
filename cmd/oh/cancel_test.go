package main

import (
	"bytes"
	"testing"

	"crdx.org/io/cmd/oh/key"
	"crdx.org/io/cmd/oh/line"
	"crdx.org/io/cmd/oh/output"
)

// Escape means "stop the current turn". At rest there is no cancellation function to call, so
// pressing it must be harmless rather than a nil-function panic.
func TestEscapeAtRestDoesNotPanic(t *testing.T) {
	self := &conversation{screen: output.New(&bytes.Buffer{})}
	input := line.NewInput(nil)

	defer func() {
		if panicValue := recover(); panicValue != nil {
			t.Fatalf("escape at rest panicked: %v", panicValue)
		}
	}()

	if !self.apply(input, nil, key.Key{Code: key.Escape}) {
		t.Error("expected escape at rest to leave the conversation open")
	}
}

// Ctrl+d on an empty line is the way out, and a turn in progress is the thing it stops first: a
// harness that exited from under a turn would leave what the turn had said unstored.
func TestControlDStopsATurnBeforeItIsAWayOut(t *testing.T) {
	self := &conversation{screen: output.New(&bytes.Buffer{})}
	input := line.NewInput(nil)

	keypress := key.Key{Code: key.Rune, Value: 'd', Mod: key.Ctrl}

	stopped := false

	self.turn = turn{running: true, stop: func() { stopped = true }}

	if !self.apply(input, nil, keypress) {
		t.Error("expected ctrl+d during a turn to stop the turn rather than the harness")
	}

	if !stopped || !self.turn.cancelled {
		t.Error("expected the turn to have been cancelled, as escape cancels it")
	}

	self.turn = turn{}

	if self.apply(input, nil, keypress) {
		t.Error("expected ctrl+d at rest to be the way out")
	}

	input.Apply(key.Key{Code: key.Rune, Value: 'a'}, false)

	if !self.apply(input, nil, keypress) {
		t.Error("expected ctrl+d on a line with something on it to leave the harness running")
	}
}

// What has been typed is not what ctrl+d is about while a turn is running: a turn is stopped by the
// key that stops turns, whether or not the next thing to say is already half written.
func TestControlDStopsATurnWhateverHasBeenTyped(t *testing.T) {
	self := &conversation{screen: output.New(&bytes.Buffer{})}
	input := line.NewInput(nil)

	input.Apply(key.Key{Code: key.Rune, Value: 'a'}, false)

	stopped := false

	self.turn = turn{running: true, stop: func() { stopped = true }}

	if !self.apply(input, nil, key.Key{Code: key.Rune, Value: 'd', Mod: key.Ctrl}) {
		t.Error("expected ctrl+d during a turn to stop the turn rather than the harness")
	}

	if !stopped || !self.turn.cancelled {
		t.Error("expected the turn to have been cancelled")
	}

	if input.Text() != "a" {
		t.Errorf("expected what was typed to be left alone, got %q", input.Text())
	}
}
