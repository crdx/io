package terminal_test

import (
	"bytes"
	"testing"

	"crdx.org/io/cmd/oh/terminal"
)

func TestCopyWritesAnOSC52ClipboardSequence(t *testing.T) {
	var output bytes.Buffer

	if err := terminal.Copy(&output, "brave-otter"); err != nil {
		t.Fatal(err)
	}

	if got, want := output.String(), "\x1b]52;c;YnJhdmUtb3R0ZXI=\x07"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
