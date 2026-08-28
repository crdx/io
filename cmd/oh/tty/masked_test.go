package tty

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestMaskedLineShowsInputAndBackspaceWithoutRevealingIt(t *testing.T) {
	var output bytes.Buffer
	value, err := readMaskedLine(strings.NewReader("ab\x7fc\r"), &output)
	if err != nil {
		t.Fatal(err)
	}
	if value != "ac" {
		t.Errorf("got value %q", value)
	}
	if output.String() != "**\b \b*\r\n" {
		t.Errorf("got rendering %q", output.String())
	}
	if strings.Contains(output.String(), value) {
		t.Error("rendering revealed the input")
	}
}

func TestMaskedLineCanBeCancelled(t *testing.T) {
	_, err := readMaskedLine(strings.NewReader("secret\x03"), &bytes.Buffer{})
	if !errors.Is(err, errInputCancelled) {
		t.Errorf("got %v", err)
	}
}
