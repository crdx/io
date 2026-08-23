package segment_test

import (
	"testing"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/segment/currentSession"
	"crdx.org/io/cmd/oh/style"
)

func TestTheSessionSegmentNamesTheSession(t *testing.T) {
	built, err := currentSession.New("brave-otter")(tomlOptions(""))
	if err != nil {
		t.Fatal(err)
	}

	if got := style.Plain(built.Render(segment.Context{})); got != "brave-otter" {
		t.Errorf("expected the session name, got %q", got)
	}
}
