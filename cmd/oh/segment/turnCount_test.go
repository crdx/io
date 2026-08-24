package segment_test

import (
	"testing"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/segment/lastTps"
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

func TestTheLastTurnSegmentSaysHowFastItCameBack(t *testing.T) {
	if got := drawn(t, lastTps.New(func() (float64, bool) { return 42.4, true }, func() bool { return false })); got != "~42t/s" {
		t.Errorf("expected a whole rate above ten, got %q", got)
	}

	if got := drawn(t, lastTps.New(func() (float64, bool) { return 4.25, true }, func() bool { return false })); got != "~4.2t/s" {
		t.Errorf("expected a tenth of a token below ten, got %q", got)
	}
}

func TestTheLastTurnSegmentShowsAnUnknownRateBeforeTheFirstTurnIsOver(t *testing.T) {
	if got := drawn(t, lastTps.New(func() (float64, bool) { return 0, false }, func() bool { return false })); got != "?t/s" {
		t.Errorf("expected an unknown rate before there is anything to say, got %q", got)
	}
}
