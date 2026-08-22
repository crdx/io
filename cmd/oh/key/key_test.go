package key

import (
	"bufio"
	"strings"
	"testing"
)

func decode(t *testing.T, input string) []Key {
	t.Helper()

	decoder := NewDecoder(bufio.NewReader(strings.NewReader(input)))

	var keypresses []Key

	for {
		next, err := decoder.Next()
		if err != nil {
			return keypresses
		}

		keypresses = append(keypresses, next)
	}
}

func TestFocusReportingIsRestoredWithTheKeyboardProtocol(t *testing.T) {
	if !strings.Contains(Enable, "\x1b[?1004h") {
		t.Errorf("focus reporting is not enabled by %q", Enable)
	}
	if !strings.Contains(Disable, "\x1b[?1004l") {
		t.Errorf("focus reporting is not disabled by %q", Disable)
	}
}

func TestEveryLineEndingIsOneEnter(t *testing.T) {
	for name, input := range map[string]string{
		"cr":   "a\rb",
		"lf":   "a\nb",
		"crlf": "a\r\nb",
	} {
		got := decode(t, input)

		want := []Key{{Code: Rune, Value: 'a'}, {Code: Enter}, {Code: Rune, Value: 'b'}}
		if len(got) != len(want) {
			t.Errorf("%s: expected %d keys, got %d: %v", name, len(want), len(got), got)
			continue
		}

		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s: expected %v, got %v", name, want[i], got[i])
			}
		}
	}
}

func TestTabArrivesAsItself(t *testing.T) {
	got := decode(t, "\t")

	if len(got) != 1 || got[0] != (Key{Code: Rune, Value: '\t'}) {
		t.Errorf("expected one tab, got %v", got)
	}
}

func TestControlCharactersStillCarryTheirModifier(t *testing.T) {
	got := decode(t, "\x03")

	if len(got) != 1 || got[0] != (Key{Code: Rune, Value: 'c', Mod: Ctrl}) {
		t.Errorf("expected ctrl+c, got %v", got)
	}
}

func TestAnEscapeOpensASequenceAndTheKeyboardProtocolReportsTheKey(t *testing.T) {
	if got := decode(t, "\x1b"); len(got) != 0 {
		t.Errorf("expected a lone escape to report nothing, got %v", got)
	}

	if got := decode(t, "\x1b[27u"); len(got) != 1 || got[0] != (Key{Code: Escape}) {
		t.Errorf("expected an escape, got %v", got)
	}
}

func TestASequenceIsNotABareEscape(t *testing.T) {
	got := decode(t, "\x1b[A\x1b[27u\x1b[200~\x1b[201~")

	want := []Key{{Code: Up}, {Code: Escape}, {Code: PasteStart}, {Code: PasteEnd}}
	if len(got) != len(want) {
		t.Fatalf("expected %d keys, got %v", len(want), got)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("expected %v, got %v", want[i], got[i])
		}
	}
}

func TestFocusChangesArriveAsKeys(t *testing.T) {
	got := decode(t, "\x1b[I\x1b[O")
	want := []Key{{Code: FocusIn}, {Code: FocusOut}}

	if len(got) != len(want) {
		t.Fatalf("expected %d focus changes, got %v", len(want), got)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("expected %v, got %v", want[i], got[i])
		}
	}
}

func TestApplicationCursorKeysAreArrows(t *testing.T) {
	for input, want := range map[string]Code{
		"\x1bOA": Up,
		"\x1bOB": Down,
		"\x1bOC": Right,
		"\x1bOD": Left,
		"\x1bOH": Home,
		"\x1bOF": End,
	} {
		keypresses := decode(t, input)

		if len(keypresses) != 1 {
			t.Errorf("%q gave %d keys, want one", input, len(keypresses))
			continue
		}

		if keypresses[0].Code != want {
			t.Errorf("%q gave %+v, want code %d", input, keypresses[0], want)
		}
	}
}

func TestOnlyTheLetterControlsCarryALetter(t *testing.T) {
	if got := plain(0); got.Code != Unknown {
		t.Errorf("plain(0) = %+v, want unknown", got)
	}

	if got := plain(1); got.Code != Rune || got.Value != 'a' || !got.Mod.Has(Ctrl) {
		t.Errorf("plain(1) = %+v, want ctrl+a", got)
	}

	if got := plain(26); got.Code != Rune || got.Value != 'z' || !got.Mod.Has(Ctrl) {
		t.Errorf("plain(26) = %+v, want ctrl+z", got)
	}
}
