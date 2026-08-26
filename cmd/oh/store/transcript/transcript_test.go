package transcript_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/store/transcript"
)

func TestTranscriptPreservesReasoningFormatting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "brave-otter", Started: time.Unix(1, 2), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(3, 4), agent.Event{Kind: agent.ModelReasoningEvent, Text: "First. Second?\nThird!"}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stored), "First. Second?\nThird!") {
		t.Errorf("reasoning formatting was not preserved:\n%s", stored)
	}
}

func TestTranscriptStoresOnlyAToolResultPreview(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "brave-otter", Started: time.Unix(1, 2), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(3, 4), agent.Event{
		Kind: agent.ToolCallResultEvent,
		ID:   "call-1",
		Name: "read",
		Text: "first\nsecond\nthird\nsecret fourth\nfifth",
	}); err != nil {
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
	if !strings.Contains(transcript, "**Output preview (first 3 lines, up to 1 KiB)**\n\n```\nfirst\nsecond\nthird\n```") {
		t.Errorf("expected a three-line tool result preview, got:\n%s", transcript)
	}
	if strings.Contains(transcript, "secret fourth") || strings.Contains(transcript, "fifth") {
		t.Errorf("expected the full tool result to be omitted, got:\n%s", transcript)
	}
	if !strings.Contains(transcript, "[`session.jsonl`](session.jsonl)") || !strings.Contains(transcript, "`event.text`") {
		t.Errorf("expected a pointer to the complete result, got:\n%s", transcript)
	}
}

func TestTranscriptCapsAToolResultPreviewAtOneKiB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "brave-otter", Started: time.Unix(1, 2), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(3, 4), agent.Event{
		Kind: agent.ToolCallResultEvent,
		ID:   "call-1",
		Name: "read",
		Text: strings.Repeat("é", 600),
	}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.Valid(stored) {
		t.Error("expected the capped transcript to remain valid UTF-8")
	}
	transcript := string(stored)
	if !strings.Contains(transcript, "\n"+strings.Repeat("é", 512)+"\n```") {
		t.Errorf("expected a one-KiB tool result preview, got:\n%s", transcript)
	}
	if strings.Contains(transcript, strings.Repeat("é", 513)) {
		t.Errorf("expected the tool result preview to stop at one KiB, got:\n%s", transcript)
	}
}

func TestTranscriptUsesAFenceLongerThanItsContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "brave-otter", Started: time.Unix(1, 2), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	content := "before\n````\nafter"
	if err := recorder.Event(time.Unix(3, 4), agent.Event{Kind: agent.ModelMessageEvent, Text: content}); err != nil {
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

func TestTranscriptRetainsTurnFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "brave-otter", Started: time.Unix(1, 2)})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(3, 4), agent.Event{
		Kind: agent.FailureEvent,
		Text: "read: connection reset by peer",
	}); err != nil {
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
	for _, want := range []string{"## Failure", "read: connection reset by peer"} {
		if !strings.Contains(transcript, want) {
			t.Errorf("expected %q in the transcript, got:\n%s", want, transcript)
		}
	}
}

func TestTranscriptNamesWhyATurnWasInterrupted(t *testing.T) {
	for name, test := range map[string]struct {
		reason string
		want   string
	}{
		"with a reason": {
			reason: "the user pressed escape",
			want:   "The turn was interrupted because the user pressed escape.",
		},
		"without a reason": {
			reason: "",
			want:   "The turn was interrupted.",
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "transcript.md")
			recorder, err := transcript.Open(path, transcript.Meta{Name: "brave-otter", Started: time.Unix(1, 2)})
			if err != nil {
				t.Fatal(err)
			}
			if err := recorder.Event(time.Unix(3, 4), agent.Event{
				Kind: agent.InterruptionEvent,
				Text: test.reason,
			}); err != nil {
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
			for _, want := range []string{"## Interrupted", test.want} {
				if !strings.Contains(transcript, want) {
					t.Errorf("expected %q in the transcript, got:\n%s", want, transcript)
				}
			}
		})
	}
}

func TestTranscriptRetainsDurableState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "brave-otter", Started: time.Unix(1, 2)})
	if err != nil {
		t.Fatal(err)
	}
	state := json.RawMessage(`{"path":"a.txt","sha256":"abc"}`)
	if err := recorder.Event(time.Unix(3, 4), agent.Event{
		Kind:  agent.StateChangeEvent,
		ID:    "call",
		Name:  "file_read",
		State: state,
	}); err != nil {
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
	for _, want := range []string{"## State", "`call`", "`file_read`", string(state)} {
		if !strings.Contains(transcript, want) {
			t.Errorf("expected %q in the transcript, got:\n%s", want, transcript)
		}
	}
}

func TestTranscriptRecordsWhatANoticeSaid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "brave-otter", Started: time.Unix(1, 2)})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(3, 4), agent.Event{
		Kind:   agent.HarnessMessageEvent,
		Status: agent.ErrorStatus,
		Text:   "the conversation could not be stored: no space left on device",
	}); err != nil {
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
	for _, want := range []string{"## Notice", "`error`", "no space left on device"} {
		if !strings.Contains(transcript, want) {
			t.Errorf("expected %q in the transcript, got:\n%s", want, transcript)
		}
	}
}

func TestTranscriptHoldsAFieldRunApartFromWhatFollowsIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "brave-otter", Started: time.Unix(1, 2)})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(3, 4), agent.Event{
		Kind:      agent.ToolCallRequestEvent,
		ID:        "call",
		Name:      "bash",
		Arguments: `{}`,
	}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(5, 6), agent.Event{Kind: caps.ModeChange, Text: "rx"}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(7, 8), agent.Event{Kind: agent.UserMessageEvent, Text: "hi"}); err != nil {
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
	for _, want := range []string{"- **Read only:** `false`\n\n**Arguments**", "- **Caps:** `rx`\n\n## User\n"} {
		if !strings.Contains(transcript, want) {
			t.Errorf("expected %q in the transcript, got:\n%s", want, transcript)
		}
	}
}
