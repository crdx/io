package bar

import (
	"testing"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
)

type fixedSegment string

func (self fixedSegment) Render(segment.Context) string {
	return string(self)
}

type fittingSegment struct {
	availableCells *int
}

func (self fittingSegment) Render(segment.Context) string {
	return "unbounded"
}

func (self fittingSegment) RenderWithin(_ segment.Context, cells int) string {
	*self.availableCells = cells
	return "+50"
}

func TestRenderWithinHandsAFittingSegmentOnlyTheRoomThatRemains(t *testing.T) {
	availableCells := 0
	layout := segment.Layout{
		segment.TopLeft: {
			fixedSegment("abc"),
			fittingSegment{availableCells: &availableCells},
		},
	}

	got := RenderWithin(layout, segment.TopLeft, segment.Context{}, 10)
	if availableCells != 4 {
		t.Errorf("fitting segment got %d cells, want 4", availableCells)
	}
	if style.Plain(got) != "abc ─ +50" || style.Width(got) > 10 {
		t.Errorf("got %q at width %d", style.Plain(got), style.Width(got))
	}
}
