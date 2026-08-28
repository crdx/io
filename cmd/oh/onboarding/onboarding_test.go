package onboarding

import (
	"bytes"
	"flag"
	"strings"
	"testing"

	"crdx.org/io/cmd/oh/link"
	"crdx.org/io/cmd/oh/model"
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

func TestNamedLoginSkipsTheProviderPicker(t *testing.T) {
	var output bytes.Buffer
	var loggedInTo string
	harry := wizard{
		output: &output,
		choose: func(string, []string) (int, error) {
			t.Fatal("provider picker was shown")
			return 0, nil
		},
		login: func(chosen provider, _ func(string)) error {
			loggedInTo = chosen.identifier
			return nil
		},
	}

	if err := harry.chooseProvider(model.AnthropicProvider); err != nil {
		t.Fatal(err)
	}
	if loggedInTo != model.AnthropicProvider {
		t.Errorf("logged in to %q", loggedInTo)
	}
	if output.String() != "✓ Signed in\n\n" {
		t.Errorf("got output %q", output.String())
	}
}

func TestNamedLoginRejectsAnUnknownProvider(t *testing.T) {
	harry := wizard{}
	if err := harry.chooseProvider("somewhere"); err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Errorf("got %v", err)
	}
}

func visibleTranscript(rendered string) string {
	rendered = link.Plain(rendered)
	rendered = strings.ReplaceAll(rendered, "\r\n", "\n")

	lines := strings.Split(rendered, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	return strings.Join(lines, "\n")
}
