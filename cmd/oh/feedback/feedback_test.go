package feedback

import (
	"strings"
	"testing"
	"time"

	"crdx.org/io/agent"
)

const testDismissAfter = 4 * time.Second

func TestOnlyCommandAndConfirmationAreDismissedByTyping(t *testing.T) {
	cases := map[Source]bool{
		System:       false,
		Command:      true,
		Config:       false,
		Confirmation: true,
	}

	for source, want := range cases {
		if got := source.IsDismissedByTyping(); got != want {
			t.Errorf("%v.IsDismissedByTyping() = %v, want %v", source, got, want)
		}
	}
}

func TestAMessageWithNoDismissAfterNeverSchedulesARefresh(t *testing.T) {
	var self State

	self.Show(System, Message{Text: "done", Status: agent.SuccessStatus}, time.Now())

	if got := self.NextRefresh(time.Now()); !got.IsZero() {
		t.Errorf("next refresh = %s, want none scheduled", got)
	}
}

func TestAMessageWithNoDismissAfterCarriesNoCountdown(t *testing.T) {
	var self State

	self.Show(System, Message{Text: "done", Status: agent.SuccessStatus}, time.Now())

	if got := renderedText(self.Render(80, time.Now())); strings.Contains(got, "dismissing") {
		t.Errorf("rendered = %q, want no countdown", got)
	}
}

func TestAMessageClearsItselfOnceItsDismissAfterElapses(t *testing.T) {
	var self State

	base := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	self.Show(Confirmation, Message{Text: "done", Status: agent.SuccessStatus, DismissAfter: testDismissAfter}, base)

	dismissingAt := base.Add(testDismissAfter)

	self.ClearExpired(dismissingAt.Add(-time.Nanosecond))
	if self.IsEmpty() {
		t.Error("feedback was cleared before its delay elapsed")
	}

	self.ClearExpired(dismissingAt)
	if !self.IsEmpty() {
		t.Error("feedback was not cleared once its delay elapsed")
	}
}

func TestAMessageTicksOnceASecondUntilItDismisses(t *testing.T) {
	var self State

	base := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	self.Show(Confirmation, Message{Text: "done", Status: agent.SuccessStatus, DismissAfter: testDismissAfter}, base)

	dismissingAt := base.Add(testDismissAfter)

	if got, want := self.NextRefresh(base), base.Add(time.Second); !got.Equal(want) {
		t.Errorf("next refresh = %s, want the next countdown tick %s", got, want)
	}

	if got := self.NextRefresh(dismissingAt.Add(-500 * time.Millisecond)); !got.Equal(dismissingAt) {
		t.Errorf("next refresh = %s, want the dismissal time once within the last second", got)
	}
}

func TestAMessageCountsDownInWholeSeconds(t *testing.T) {
	var self State

	base := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	self.Show(Confirmation, Message{Text: "done", Status: agent.SuccessStatus, DismissAfter: testDismissAfter}, base)

	if got, want := renderedText(self.Render(80, base)), "(dismissing in 4s)"; !strings.Contains(got, want) {
		t.Errorf("rendered = %q, want it to contain %q", got, want)
	}

	afterThreeAndAHalfSeconds := base.Add(3500 * time.Millisecond)
	if got, want := renderedText(self.Render(80, afterThreeAndAHalfSeconds)), "(dismissing in 1s)"; !strings.Contains(got, want) {
		t.Errorf("rendered = %q, want it to contain %q", got, want)
	}
}

func renderedText(rows []string) string {
	return strings.Join(rows, "\n")
}
