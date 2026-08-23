package dynamic

import (
	"strings"
	"testing"
	"time"

	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/width"
)

const wide = 100

func testBlock() *Block {
	return &Block{refresh: func() {}, stop: make(chan struct{})}
}

type textLabel struct {
	text string
}

func (self textLabel) Elide(room int) Label { return textLabel{text: width.Elide(self.text, room)} }

func (self textLabel) Render() string { return self.text }

func (self textLabel) Width() int { return width.Of(self.text) }

func rowLabel(name string, subject string) Label {
	return textLabel{text: name + " " + subject}
}

func TestARowSaysNothingOfItsProgressUntilItHasBeenGoingAWhile(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("read", "main.go"))

	if got := block.Rows(wide)[0]; got != "read main.go" {
		t.Errorf("expected the label and nothing else, got %q", got)
	}
}

func TestAQuickRowIsMarkedWithoutATime(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("read", "main.go"))

	block.FinaliseRow(0, Done, 500*time.Millisecond, "", "")

	if got := block.Rows(wide)[0]; !strings.Contains(got, "✓") || strings.Contains(got, "s") {
		t.Errorf("expected a mark and no time, got %q", got)
	}
}

func TestARowWorthWaitingForSaysHowLongItTook(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("read", "main.go"))

	block.FinaliseRow(0, Done, 5*time.Second, "", "")

	if got := block.Rows(wide)[0]; !strings.Contains(got, style.Spinner("5s")) {
		t.Errorf("expected the time it took, got %q", got)
	}
}

func TestRunningTimerUsesWholeSecondGranularity(t *testing.T) {
	block := &Block{isSlow: true}
	item := row{state: Running, startedAt: time.Now().Add(-5500 * time.Millisecond)}

	if got := style.Plain(block.getResult(item)); got != "✦· 5s" {
		t.Errorf("expected a whole-second timer, got %q", got)
	}
}

func TestARowThatFailedIsMarkedApartFromOneThatDidNot(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("read", "main.go"))
	block.Add(rowLabel("read", "nowhere.go"))

	block.FinaliseRow(0, Done, 0, "", "")
	block.FinaliseRow(1, Failed, 0, "", "")

	rows := block.Rows(wide)

	if !strings.Contains(rows[0], "✓") || !strings.Contains(rows[1], "✗") {
		t.Errorf("expected a tick and a cross, got %q", rows)
	}
}

func TestARowThatFailedSaysWhy(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("write", "main.go"))

	block.FinaliseRow(0, Failed, 0, "permission denied", "")

	if got := block.Rows(wide)[0]; !strings.Contains(got, style.Failure("permission denied")) {
		t.Errorf("expected the reason in the colour of a failure, got %q", got)
	}
}

func TestAReasonIsPutOnTheOneRow(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("bash", "make"))

	block.FinaliseRow(0, Failed, 0, "no rule to make target\n\tstop.\r", "")

	if got := block.Rows(wide)[0]; strings.ContainsAny(got, "\n\r\t") {
		t.Errorf("expected one row, got %q", got)
	}
}

func TestAReasonTakesTheRoomTheLabelDoesNotWant(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("read", "main.go"))

	reason := strings.Repeat("a", wide/2+10)

	block.FinaliseRow(0, Failed, 0, reason, "")

	if got := block.Rows(wide)[0]; !strings.Contains(got, reason) {
		t.Errorf("expected the whole reason, got %q", got)
	}
}

func TestALongReasonLeavesTheLabelItIsAbout(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("write", "main.go"))

	block.FinaliseRow(0, Failed, 0, strings.Repeat("wordy ", wide), "")

	row := block.Rows(wide)[0]

	if style.Width(row) > wide {
		t.Errorf("expected the row to fit in %d columns, got %d in %q", wide, style.Width(row), row)
	}

	if !strings.Contains(row, "main.go") {
		t.Errorf("expected the call to survive the reason, got %q", row)
	}
}

func TestAReasonIsKeptOnlyForAFailure(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("read", "main.go"))

	block.FinaliseRow(0, Done, 0, "read 40 lines", "")

	if got := block.Rows(wide)[0]; strings.Contains(got, "read 40 lines") {
		t.Errorf("expected nothing beside the mark, got %q", got)
	}
}

func TestClosingTheBlockMarksWhateverWasStillRunning(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("read", "main.go"))
	block.Add(rowLabel("grep", "spinner"))
	block.FinaliseRow(0, Done, 0, "", "")

	block.Close(Cancelled)

	rows := block.Rows(wide)

	if !strings.Contains(rows[0], "✓") {
		t.Errorf("expected the call that finished to keep its tick, got %q", rows[0])
	}

	if !strings.Contains(rows[1], "–") {
		t.Errorf("expected the call still running to be marked cancelled, got %q", rows[1])
	}
}

func TestARowIsMarkedOnlyOnce(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("read", "main.go"))
	block.FinaliseRow(0, Failed, 0, "", "")

	block.FinaliseRow(0, Done, 0, "", "")

	if got := block.Rows(wide)[0]; !strings.Contains(got, "✗") {
		t.Errorf("expected the first mark to stand, got %q", got)
	}
}

func TestALabelIsCutToLeaveRoomForTheOutcome(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("grep", strings.Repeat("x", wide)))

	block.FinaliseRow(0, Done, 3*time.Second, "", "")

	for _, row := range block.Rows(wide) {
		if style.Width(row) > wide {
			t.Errorf("expected the row to fit %d columns, got %d in %q", wide, style.Width(row), row)
		}
	}
}

func TestALabelTakesTheRoomATimeWouldHaveTakenWhereNoTimeIsShown(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("bash", strings.Repeat("x", wide)))

	block.FinaliseRow(0, Done, time.Millisecond, "", "")

	for _, row := range block.Rows(wide) {
		if style.Width(row) != wide-edgeGuard {
			t.Errorf(
				"expected the row to leave its %d-column edge guard, got width %d in %q",
				edgeGuard, style.Width(row), row,
			)
		}
	}
}

func TestALabelGivesRoomBackWhenTheTimeAppears(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("bash", strings.Repeat("x", wide)))

	block.FinaliseRow(0, Done, 3*time.Second, "", "")

	for _, row := range block.Rows(wide) {
		if style.Width(row) != wide-edgeGuard {
			t.Errorf(
				"expected the row to leave its %d-column edge guard, got width %d in %q",
				edgeGuard, style.Width(row), row,
			)
		}
	}
}

func TestACompletedOutcomeIsKeptBackFromTheTerminalEdge(t *testing.T) {
	block := testBlock()
	const narrow = 67

	block.Add(rowLabel("grep", "RESOLVE_UNIX|resolve_unix|unix.*socket|Landlock|landlock"))

	block.FinaliseRow(0, Done, 7420*time.Millisecond, "", "")

	row := block.Rows(narrow)[0]
	if got := style.Plain(row); !strings.HasSuffix(got, "✓ 7.4s") {
		t.Errorf("expected the complete outcome at the end, got %q", got)
	}
	if got := style.Width(row); got > narrow-edgeGuard {
		t.Errorf("expected %d guarded columns at the edge, got width %d in %q", edgeGuard, got, row)
	}
}

func TestARowTooNarrowForWhatItMeasuredKeepsItsLabelAndItsMark(t *testing.T) {
	const tiny = 12

	block := testBlock()

	index := block.Add(rowLabel("bash", "if [[ -f one ]]; then echo one; fi"))
	block.FinaliseRow(index, Done, time.Second, "", "900L+ ~500t (of ~225Kt)")

	row := block.Rows(tiny)[index]

	if got := style.Width(row); got > tiny {
		t.Errorf("expected the row to fit %d columns, got %d in %q", tiny, got, row)
	}

	plain := style.Plain(row)
	if !strings.HasPrefix(plain, "bash") || !strings.HasSuffix(plain, "✓") {
		t.Errorf("expected the call and its mark, got %q", plain)
	}
}

func TestARowWideEnoughKeepsEverythingItMeasured(t *testing.T) {
	block := testBlock()

	index := block.Add(rowLabel("bash", "echo one"))
	block.FinaliseRow(index, Done, time.Second, "", "1L ~1t")

	if plain := style.Plain(block.Rows(wide)[index]); !strings.HasSuffix(plain, "✓ 1L ~1t") {
		t.Errorf("expected everything the call measured, got %q", plain)
	}
}
