package main

import (
	"bytes"
	"testing"

	"crdx.org/io/cmd/oh/key"
	"crdx.org/io/cmd/oh/line"
	"crdx.org/io/cmd/oh/output"
)

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
