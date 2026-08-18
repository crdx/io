package picker

import (
	"strings"
	"testing"

	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/cmd/oh/theme"
)

func TestClipReturnsNothingWhenThereAreNoColumns(t *testing.T) {
	if got := clip("session", 0); got != "" {
		t.Errorf("expected no text in no columns, got %q", got)
	}
}

func TestAnUntitledSessionDoesNotPutAnEscapeSequenceThroughTheClipper(t *testing.T) {
	got := title(&store.Session{})

	if strings.ContainsRune(got, '\x1b') {
		t.Errorf("expected an unpainted title, got %q", got)
	}

	if theme.Width(got) != len("(nothing was asked)") {
		t.Errorf("expected the placeholder title, got %q", got)
	}
}
