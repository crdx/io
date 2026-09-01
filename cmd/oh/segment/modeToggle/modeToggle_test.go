package modeToggle_test

import (
	"testing"

	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/segment/modeToggle"
	"crdx.org/io/cmd/oh/style"
)

func TestEveryCapabilitySetKeepsTheSameLetters(t *testing.T) {
	for _, flags := range []string{"", "r", "rw", "rx", "rxw", "rxwg", "rxwgs", "rg", "rs"} {
		grantedCaps, err := caps.Parse(flags)
		if err != nil {
			t.Fatal(err)
		}

		if got := style.Plain(render(t, grantedCaps, false)); got != "rxw gs" {
			t.Errorf("caps %q drew %q, want the letters to stand whatever is granted", flags, got)
		}
	}
}

func TestOnlyTheStylingSaysWhatIsGranted(t *testing.T) {
	refused := render(t, caps.Read, false)
	granted := render(t, caps.Read|caps.Write, false)

	if refused == granted {
		t.Errorf("granting the write capability drew nothing new: %q", granted)
	}

	pending := render(t, caps.Read, true)
	if pending == refused {
		t.Errorf("a pending prefix drew nothing new: %q", pending)
	}
	if got := style.Plain(pending); got != "rxw gs" {
		t.Errorf("a pending prefix drew %q, want the same letters", got)
	}
}

type noOptions struct{}

func (noOptions) Read(any) error {
	return nil
}

func render(t *testing.T, grantedCaps caps.Set, isPrefixPending bool) string {
	t.Helper()

	built, err := modeToggle.New(
		func() caps.Set { return grantedCaps },
		func() bool { return isPrefixPending },
	)(noOptions{})
	if err != nil {
		t.Fatal(err)
	}

	return built.Render(segment.Context{})
}
