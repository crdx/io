package tty

import (
	"errors"
	"io"
	"testing"
	"time"
)

func TestAStoppedReaderLeavesTheTerminalToTheNextReader(t *testing.T) {
	terminal := pty(t)

	reader := NewReader(terminal)
	stopped := make(chan error, 1)

	go func() {
		var buffer [1]byte
		_, err := reader.Read(buffer[:])
		stopped <- err
	}()

	time.Sleep(2 * readPollInterval)
	reader.Stop()

	select {
	case err := <-stopped:
		if !errors.Is(err, io.EOF) {
			t.Errorf("got %v, want EOF", err)
		}
	case <-time.After(time.Second):
		t.Fatal("a stopped read did not return")
	}

	if _, err := io.WriteString(terminal, "x"); err != nil {
		t.Fatal(err)
	}

	var taken [1]byte
	if _, err := terminal.Read(taken[:]); err != nil {
		t.Fatal(err)
	}
	if taken[0] != 'x' {
		t.Errorf("the next reader got %q, so the stopped one was still holding the terminal", taken[0])
	}
}

func TestAReaderHandsOnWhatTheTerminalSends(t *testing.T) {
	terminal := pty(t)

	if _, err := io.WriteString(terminal, "hi"); err != nil {
		t.Fatal(err)
	}

	reader := NewReader(terminal)
	t.Cleanup(reader.Stop)

	var buffer [2]byte
	read, err := io.ReadFull(reader, buffer[:])
	if err != nil {
		t.Fatal(err)
	}
	if got := string(buffer[:read]); got != "hi" {
		t.Errorf("got %q", got)
	}
}
