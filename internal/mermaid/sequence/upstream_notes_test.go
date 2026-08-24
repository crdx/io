package sequence

import (
	"strings"
	"testing"
)

func firstNote(sd *SequenceDiagram) *Note {
	for _, ev := range sd.Events {
		if ev.Kind == EventNote {
			return ev.Note
		}
	}
	return nil
}

func TestParseNotes(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantPlace  NotePlacement
		wantActors []string
		wantText   string
	}{
		{"over single", "sequenceDiagram\n Note over B: hi", NoteOver, []string{"B"}, "hi"},
		{"over multiple", "sequenceDiagram\n Note over A,B: shared", NoteOver, []string{"A", "B"}, "shared"},
		{"over reversed", "sequenceDiagram\n Note over B,A: rev", NoteOver, []string{"B", "A"}, "rev"},
		{"left of", "sequenceDiagram\n Note left of A: L", NoteLeftOf, []string{"A"}, "L"},
		{"right of", "sequenceDiagram\n Note right of A: R", NoteRightOf, []string{"A"}, "R"},
		{"lowercase note", "sequenceDiagram\n note over A: low", NoteOver, []string{"A"}, "low"},
		{"special chars", "sequenceDiagram\n Note over A: <>&! 100%", NoteOver, []string{"A"}, "<>&! 100%"},
		{"br collapses", "sequenceDiagram\n Note over A: line1<br/>line2", NoteOver, []string{"A"}, "line1<br/>line2"},
		{"nowrap prefix stripped", "sequenceDiagram\n Note right of B:nowrap: hi there", NoteRightOf, []string{"B"}, "hi there"},
		{"wrap prefix stripped", "sequenceDiagram\n Note over A:wrap: hi", NoteOver, []string{"A"}, "hi"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sd, err := Parse(tt.input)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			n := firstNote(sd)
			if n == nil {
				t.Fatal("no note event parsed")
			}
			if n.Placement != tt.wantPlace {
				t.Errorf("placement = %v, want %v", n.Placement, tt.wantPlace)
			}
			if len(n.Participants) != len(tt.wantActors) {
				t.Fatalf("actors = %d, want %d", len(n.Participants), len(tt.wantActors))
			}
			for i, a := range tt.wantActors {
				if n.Participants[i].ID != a {
					t.Errorf("actor %d = %q, want %q", i, n.Participants[i].ID, a)
				}
			}
			if n.Text != tt.wantText {
				t.Errorf("text = %q, want %q", n.Text, tt.wantText)
			}
		})
	}
}

func TestParseNoteErrors(t *testing.T) {
	if _, err := Parse("sequenceDiagram\n Note over : x"); err == nil {
		t.Error("expected error for note with no participant")
	}
}

func TestRenderNoteSmoke(t *testing.T) {
	sd, err := Parse("sequenceDiagram\n A->>B: x\n Note over A,B: a<br/>b\n Note right of B: r")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, ascii := range []bool{false, true} {
		out, err := Render(sd, upstreamTestConfig(ascii))
		if err != nil {
			t.Fatalf("render ascii=%v: %v", ascii, err)
		}
		if !strings.Contains(out, "a b") {
			t.Errorf("ascii=%v: expected collapsed note text 'a b' in output:\n%s", ascii, out)
		}
	}
}

func TestNoteLeftOfLeftmostKeepsLifelines(t *testing.T) {
	sd, err := Parse("sequenceDiagram\n A->>B: x\n Note left of A: LEFT")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := Render(sd, upstreamTestConfig(true))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{"| A |", "| B |", "LEFT"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "\n+") && !strings.HasPrefix(out, "+") {
		t.Errorf("expected the left-of note box to start at column 0:\n%s", out)
	}
}

func TestNoteInsideFragmentNotCutByBorder(t *testing.T) {
	sd, err := Parse("sequenceDiagram\n loop retry\n  A->>B: try\n  Note left of A: hello world\n end")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := Render(sd, upstreamTestConfig(true))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(out, "hello world") {
		t.Errorf("note text should be intact inside the frame:\n%s", out)
	}
}

func TestNoteInNestedFragmentsStagger(t *testing.T) {
	sd, err := Parse("sequenceDiagram\n loop outer\n  opt inner\n   A->>B: x\n   Note left of A: hi\n  end\n end")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := Render(sd, upstreamTestConfig(false))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{"hi", "[loop outer]", "[opt inner]"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q intact in output:\n%s", want, out)
		}
	}
	lines := strings.Split(out, "\n")
	col := func(sub string) int {
		for _, l := range lines {
			if i := strings.Index(l, sub); i >= 0 {
				return i
			}
		}
		return -1
	}
	loopCol, optCol := col("┌─[loop outer]"), col("┌─[opt inner]")
	if loopCol < 0 || optCol < 0 || optCol <= loopCol {
		t.Errorf("frames should stagger (loop=%d < opt=%d):\n%s", loopCol, optCol, out)
	}
}
