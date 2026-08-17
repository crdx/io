package tty

import (
	"bytes"
	"errors"
	"os"
	"testing"
)

func TestNothingButAFileCanBeATerminal(t *testing.T) {
	for name, stream := range map[string]any{
		"a buffer":   &bytes.Buffer{},
		"nothing":    nil,
		"a pipe end": pipe(t),
	} {
		if Is(stream) {
			t.Errorf("%s: expected no terminal", name)
		}
	}
}

// Raw writes the protocol to the screen, so a screen that is not the terminal would send it
// somewhere the terminal never hears, and leave the escapes in whatever it was redirected to.
func TestRawRefusesAScreenThatIsNotATerminal(t *testing.T) {
	var screen bytes.Buffer

	restore, err := Raw(pty(t), &screen)
	if !errors.Is(err, ErrNotTerminal) {
		t.Errorf("expected the terminal to be refused, got %v", err)
	}

	if restore != nil {
		t.Error("expected nothing to undo")
	}

	if screen.Len() != 0 {
		t.Errorf("expected nothing written, got %q", screen.String())
	}
}

func pty(t *testing.T) *os.File {
	t.Helper()

	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal to test against: %v", err)
	}

	t.Cleanup(func() { _ = master.Close() })

	if !Is(master) {
		t.Fatal("expected the master end of a pseudo-terminal to be a terminal")
	}

	return master
}

func pipe(t *testing.T) *os.File {
	t.Helper()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Cleanup(func() {
		_ = reader.Close()
		_ = writer.Close()
	})

	return writer
}
