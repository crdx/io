package segment_test

import (
	"testing"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/segment/sessionEmoji"
	"crdx.org/io/cmd/oh/style"
)

func TestTheSessionEmojiSegmentStandsForTheAnimal(t *testing.T) {
	built, err := sessionEmoji.New("brave-otter")(tomlOptions(""))
	if err != nil {
		t.Fatal(err)
	}

	if got := style.Plain(built.Render(segment.Context{})); got != "🦦" {
		t.Errorf("expected the otter emoji, got %q", got)
	}
}

func TestTheSessionEmojiSegmentDrawsNothingForAnUnknownAnimal(t *testing.T) {
	built, err := sessionEmoji.New("brave-tester")(tomlOptions(""))
	if err != nil {
		t.Fatal(err)
	}

	if got := built.Render(segment.Context{}); got != "" {
		t.Errorf("expected nothing, got %q", got)
	}
}
