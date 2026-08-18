package chat_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/store/chat"
)

func TestTranscriptUsesAFenceLongerThanItsContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat.md")
	recorder, err := chat.Open(path, chat.Meta{ID: "session", Started: time.Unix(1, 2), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	content := "before\n````\nafter"
	if err := recorder.Event(time.Unix(3, 4), agent.Event{Kind: agent.Text, Text: content}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	transcript := string(stored)
	if !strings.Contains(transcript, "`````\n"+content+"\n`````") {
		t.Errorf("expected a five-backtick fence, got:\n%s", transcript)
	}
	if !strings.Contains(transcript, "# Conversation") || !strings.Contains(transcript, "## Assistant") {
		t.Errorf("expected the metadata and event headings, got:\n%s", transcript)
	}
}
