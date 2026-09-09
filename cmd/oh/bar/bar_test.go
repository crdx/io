package bar

import (
	"os"
	"path/filepath"
	"testing"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/work"
	"crdx.org/io/internal/util/strutil"
)

type fixedSegment string

func (self fixedSegment) Render(segment.Context) string {
	return string(self)
}

func fixedFactory(value string) segment.Factory {
	return func(segment.Options) (segment.Segment, error) {
		return fixedSegment(value), nil
	}
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

func TestTheFastModeSegmentIsRegisteredWithTheCurrentSelection(t *testing.T) {
	for _, isFast := range []bool{false, true} {
		registry := NewRegistry(Options{Workspace: work.At(t.TempDir()), IsFast: isFast})
		builtSegment, err := registry[fastModeSegment](nil)
		if err != nil {
			t.Fatal(err)
		}

		got := style.Plain(builtSegment.Render(segment.Context{}))
		if isFast && got != "⚡" || !isFast && got != "·" {
			t.Errorf("fast=%t drew %q", isFast, got)
		}
	}
}

func TestInfoDrawsEveryAvailableNonemptySegmentAndSummarisesTheEmptyOnes(t *testing.T) {
	cacheValue := "\x1b[31m5m ttl\x1b[0m"
	modeValue := "\x1b[32mrxw gs\x1b[0m"
	registry := segment.Registry{
		activitySpinnerSegment: fixedFactory("·✦·"),
		cacheUsageSegment:      fixedFactory("unused configured value"),
		jobNamesSegment:        fixedFactory(""),
		modeToggleSegment:      fixedFactory(modeValue),
		scrollOverflowSegment:  fixedFactory("↑ 3"),
	}
	configuration := NewConfiguration(registry, segment.Layout{
		segment.TopCenter: {
			segment.Instance{Name: cacheUsageSegment, Segment: fixedSegment(cacheValue)},
		},
	})

	info, err := configuration.RenderInfo(segment.Context{})
	if err != nil {
		t.Fatal(err)
	}
	got := strutil.VisibleEscapes(info) + "\n"
	want, err := os.ReadFile(filepath.Join("testdata", "info.ansi"))
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderWithinHandsAFittingSegmentOnlyTheRoomThatRemains(t *testing.T) {
	availableCells := 0
	layout := segment.Layout{
		segment.TopLeft: {
			segment.Instance{Name: "fixed", Segment: fixedSegment("abc")},
			segment.Instance{
				Name:    "fitting",
				Segment: fittingSegment{availableCells: &availableCells},
			},
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
