package segment_test

import (
	"testing"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/segment/turnCount"
	"crdx.org/io/cmd/oh/style"
)

func drawn(t *testing.T, factory segment.Factory) string {
	t.Helper()

	built, err := factory(tomlOptions(""))
	if err != nil {
		t.Fatal(err)
	}

	return style.Plain(built.Render(segment.Context{}))
}

func TestTheTurnCountSegmentCountsFromTheFirstTurn(t *testing.T) {
	if got := drawn(t, turnCount.New(func() int { return 3 })); got != "#3" {
		t.Errorf("expected the third turn to be marked, got %q", got)
	}

	if got := drawn(t, turnCount.New(func() int { return 0 })); got != "" {
		t.Errorf("expected a session with nothing asked of it to say nothing, got %q", got)
	}
}
