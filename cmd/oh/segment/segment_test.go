package segment_test

import (
	"errors"
	"strings"
	"testing"

	"crdx.org/io/cmd/oh/segment"
)

type saying struct {
	text string
}

func (self saying) Render(segment.Context) string {
	return self.text
}

func offering(text string) segment.Factory {
	return func(unmarshall segment.Unmarshall) (segment.Instance, error) {
		if err := unmarshall(&struct{}{}); err != nil {
			return nil, err
		}

		return saying{text: text}, nil
	}
}

func nothing(any) error {
	return nil
}

func TestEveryPositionIsListedExactlyOnce(t *testing.T) {
	if len(segment.Positions) != 6 {
		t.Errorf("expected six positions, got %d", len(segment.Positions))
	}

	seen := map[segment.Position]bool{}
	for _, position := range segment.Positions {
		if seen[position] {
			t.Errorf("expected %s once, got it twice", position)
		}
		seen[position] = true
	}
}

func TestASegmentOnOfferIsBuilt(t *testing.T) {
	set := segment.Set{"model": offering("gpt")}

	built, err := set.Build("model", segment.BottomLeft, nothing)
	if err != nil {
		t.Fatal(err)
	}

	if got := built.Render(segment.Context{}); got != "gpt" {
		t.Errorf("expected the built segment to draw itself, got %q", got)
	}
}

func TestASegmentThatIsNotOfferedSaysWhereAndWhatInstead(t *testing.T) {
	set := segment.Set{"model": offering("gpt"), "scroll": offering("↑ 3")}

	_, err := set.Build("weather", segment.BottomCenter, nothing)
	if err == nil {
		t.Fatal("expected an unknown segment to be refused")
	}

	for _, want := range []string{"bottom.center", "weather", "model, scroll"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected %q to mention %q", err, want)
		}
	}
}

func TestASegmentRefusingItsOptionsSaysWhereTheyWereWritten(t *testing.T) {
	refuses := func(segment.Unmarshall) (segment.Instance, error) {
		return nil, errors.New("no shouting")
	}

	_, err := segment.Set{"model": refuses}.Build("model", segment.TopRight, nothing)
	if err == nil {
		t.Fatal("expected the refusal to be reported")
	}

	for _, want := range []string{"top.right", "model", "no shouting"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected %q to mention %q", err, want)
		}
	}
}

func TestTheNamesOnOfferAreListedInOrder(t *testing.T) {
	set := segment.Set{"model": offering(""), "scroll": offering(""), "modes": offering("")}

	got := strings.Join(set.Names(), ",")
	if want := "model,modes,scroll"; got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}
