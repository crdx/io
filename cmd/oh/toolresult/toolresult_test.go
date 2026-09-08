package toolresult

import (
	"flag"
	"strings"
	"testing"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/style"
	internaltoolresult "crdx.org/io/internal/toolresult"
	"crdx.org/io/session"
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

func TestRunWritesSafeToolOutput(t *testing.T) {
	directory, address := toolResultFixture(t)

	var output strings.Builder
	err := run(&inputOpts{
		ToolResult: true,
		URL:        address,
	}, directory, &output, nil)
	if err != nil {
		t.Fatalf("unexpected run error: %v", err)
	}
	want := "read · notes.txt\n\none\ntwo\n"
	if style.Plain(output.String()) != want {
		t.Errorf("got output %q", output.String())
	}
	if strings.Contains(output.String(), "example.test") {
		t.Errorf("unsafe hyperlink survived in %q", output.String())
	}
}

func TestPagerReceivesSafeToolOutput(t *testing.T) {
	directory, address := toolResultFixture(t)
	pagedText := ""
	openPager := func(text string) error {
		pagedText = text
		return nil
	}

	var output strings.Builder
	err := run(&inputOpts{
		ToolResult: true,
		Pager:      true,
		URL:        address,
	}, directory, &output, openPager)
	if err != nil {
		t.Fatalf("unexpected run error: %v", err)
	}
	if style.Plain(pagedText) != "read · notes.txt\n\none\ntwo\n" {
		t.Errorf("paged text is %q", pagedText)
	}
	if output.Len() != 0 {
		t.Errorf("standard output contains %q", output.String())
	}
}

func toolResultFixture(t *testing.T) (string, string) {
	t.Helper()

	directory := t.TempDir()
	writer, err := session.Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Event(agent.Event{
		Kind:      agent.ToolCallRequestEvent,
		ID:        "call-1",
		Name:      "read",
		Arguments: `{"path":"notes.txt"}`,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Event(agent.Event{
		Kind:   agent.ToolCallResultEvent,
		ID:     "call-1",
		Name:   "read",
		Status: agent.SuccessStatus,
		Text:   "one\n\x1b]8;;https://example.test\x1b\\two\x1b]8;;\x1b\\\n",
	}); err != nil {
		t.Fatal(err)
	}
	name := writer.Name()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	return directory, internaltoolresult.URL(name, "call-1")
}
