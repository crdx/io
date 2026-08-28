package escape

import "testing"

func TestGetEndIncludesTheEscapeTerminator(t *testing.T) {
	for _, sequence := range []string{"\x1b[38;2;255;255;255m", "\x1b[K"} {
		runes := []rune(sequence + "after")
		if got := GetEnd(runes, 0); got != len([]rune(sequence)) {
			t.Errorf("GetEnd(%q) = %d, want %d", sequence, got, len([]rune(sequence)))
		}
	}
}

func TestGetEndTakesAnUnterminatedEscapeToTheEnd(t *testing.T) {
	runes := []rune("\x1b[38;2")
	if got := GetEnd(runes, 0); got != len(runes) {
		t.Errorf("GetEnd() = %d, want %d", got, len(runes))
	}
}

func TestGetSequenceDescribesSizedText(t *testing.T) {
	sequence := "\x1b]66;s=2:n=3:d=4:w=2;🐟\x1b\\"
	runes := []rune(sequence + "after")
	got := GetSequence(runes, 0)

	if got.End != len([]rune(sequence)) {
		t.Errorf("end = %d, want %d", got.End, len([]rune(sequence)))
	}
	if got.Text != "🐟" {
		t.Errorf("text = %q, want fish", got.Text)
	}
	if got.Cells != 4 {
		t.Errorf("cells = %d, want 4", got.Cells)
	}
}

func TestGetSequenceDescribesRightwardCursorMovement(t *testing.T) {
	got := GetSequence([]rune("\x1b[4Cafter"), 0)
	if got.End != len([]rune("\x1b[4C")) || got.Cells != 4 || got.IsStyle {
		t.Errorf("GetSequence() = %+v, want a four-cell cursor move", got)
	}
}

func TestMalformedSizedTextOccupiesNoCells(t *testing.T) {
	for _, sequence := range []string{
		"\x1b]66;s=2:w=2;🐟",
		"\x1b]66;s=0:w=2;🐟\x1b\\",
		"\x1b]66;s=8:w=2;🐟\x1b\\",
		"\x1b]66;s=no:w=2;🐟\x1b\\",
		"\x1b]66;s=2;🐟\x1b\\",
		"\x1b]66;s=2:w=0;🐟\x1b\\",
		"\x1b]66;s=2:w=8;🐟\x1b\\",
		"\x1b]66;s=2:w=no;🐟\x1b\\",
		"\x1b]66;s=2:w=2;\x1b\\",
	} {
		got := GetSequence([]rune(sequence), 0)
		if got.Cells != 0 || got.Text != "" {
			t.Errorf("GetSequence(%q) = %+v, want no visible cells", sequence, got)
		}
	}
}

func FuzzGetSequence(fuzzer *testing.F) {
	for _, text := range []string{
		"plain",
		"\x1b",
		"\x1b[31mred\x1b[0m",
		"\x1b[4Cright",
		"\x1b[999999999999999999999Cright",
		"\x1b]66;s=2:w=2;🐟\x1b\\",
		"\x1b]66;s=-999999999999999999:w=2;x",
	} {
		fuzzer.Add(text)
	}

	fuzzer.Fuzz(func(t *testing.T, text string) {
		runes := []rune(text)
		for start, character := range runes {
			if character != '\x1b' {
				continue
			}

			sequence := GetSequence(runes, start)
			if sequence.End <= start || sequence.End > len(runes) {
				t.Fatalf("GetSequence(%q, %d) ended at %d", text, start, sequence.End)
			}
			if sequence.Cells < 0 || sequence.Cells > maximumCursorCells {
				t.Fatalf("GetSequence(%q, %d) occupies %d cells", text, start, sequence.Cells)
			}
			if sequence.Text != "" && sequence.Cells > 49 {
				t.Fatalf("GetSequence(%q, %d) has oversized text: %+v", text, start, sequence)
			}
			if sequence.Cells == 0 && sequence.Text != "" {
				t.Fatalf("GetSequence(%q, %d) has text without cells: %+v", text, start, sequence)
			}
			if sequence.IsStyle && sequence.Cells != 0 {
				t.Fatalf("GetSequence(%q, %d) is both style and text: %+v", text, start, sequence)
			}
		}
	})
}
