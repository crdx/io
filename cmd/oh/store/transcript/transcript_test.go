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
	"crdx.org/io/tool"
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

func TestTranscriptRoundsShortElapsedTimesToTenths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	startedAt := time.Unix(1, 0)
	recorder, err := transcript.Open(path, transcript.Meta{Name: "brave-otter", Started: startedAt, Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(startedAt.Add(50*time.Millisecond), agent.Event{Kind: agent.ModelMessageEvent, Text: "answer"}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stored), "## Assistant · +0.1s") {
		t.Errorf("short elapsed time was not rendered to tenths:\n%s", stored)
	}
}

func TestTranscriptNamesACallInItsHeadingAndReadsItsMeasurementsOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "brave-otter", Started: time.Unix(1, 2), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	request := agent.Event{Kind: agent.ToolCallRequestEvent, ID: "call-1", Name: "bash", Arguments: `{"command":"ls"}`}
	request.Subject = "ls"
	request.Emphasis = tool.Emphasis{Kind: tool.EmphasisSyntax, Value: "bash"}
	if err := recorder.Event(time.Unix(3, 4), request); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(5, 6), agent.Event{
		Kind:   agent.ToolCallResultEvent,
		ID:     "call-1",
		Name:   "bash",
		Status: agent.SuccessStatus,
		Took:   12 * time.Second,
		Stats: &tool.Stats{
			Kind:       tool.StatsResources,
			Lines:      1,
			Bytes:      2048,
			TotalBytes: 4096,
			CPUTime:    2500 * time.Millisecond,
			PeakMemory: 4 << 20,
			Truncated:  true,
		},
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
	for _, want := range []string{
		"## Tool call — bash · +2.0s\n\n```bash\nls\n```",
		"→ success in 12s, 1 line, 2K of 4K, 2.5s CPU, 4M peak, truncated · +4.0s\n",
	} {
		if !strings.Contains(transcript, want) {
			t.Errorf("expected %q in the transcript, got:\n%s", want, transcript)
		}
	}
	if strings.Contains(transcript, `{"command":"ls"}`) {
		t.Errorf("expected the arguments to give way to the rendered call, got:\n%s", transcript)
	}
}

func TestTranscriptFallsBackToTheArgumentsOfAnUnrenderedCall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "brave-otter", Started: time.Unix(1, 2), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(3, 4), agent.Event{
		Kind:      agent.ToolCallRequestEvent,
		ID:        "call-1",
		Name:      "summon",
		Arguments: `{"name":"one"}`,
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
	if !strings.Contains(string(stored), "```json\n{\"name\":\"one\"}\n```") {
		t.Errorf("expected the arguments to stand in for a call that renders nothing, got:\n%s", stored)
	}
}

func TestTranscriptHoldsAShortRenderedCallOnItsHeadingAndShowsAWholeResultAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "brave-otter", Started: time.Unix(1, 2), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	request := agent.Event{Kind: agent.ToolCallRequestEvent, ID: "call-1", Name: "read"}
	request.Subject = "draw.go"
	if err := recorder.Event(time.Unix(3, 4), request); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(5, 6), agent.Event{
		Kind:   agent.ToolCallResultEvent,
		ID:     "call-1",
		Name:   "read",
		Status: agent.SuccessStatus,
		Text:   "package main\n",
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
	if !strings.Contains(transcript, "## Tool call — read · `draw.go` · +2.0s\n\n→ success · +4.0s\n\n```\npackage main\n```") {
		t.Errorf("expected the call and its answer to be held on two lines, got:\n%s", transcript)
	}
	if strings.Contains(transcript, "preview") || strings.Contains(transcript, "jq") {
		t.Errorf("expected nothing to point elsewhere for output that is all there, got:\n%s", transcript)
	}
}

func TestTranscriptFencesARenderedCallTooLongForAHeading(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "brave-otter", Started: time.Unix(1, 2), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	subject := "/home/alice/" + strings.Repeat("deeply/", 20) + "draw.go"
	request := agent.Event{Kind: agent.ToolCallRequestEvent, ID: "call-1", Name: "read"}
	request.Subject = subject
	request.Note = "1-200"
	if err := recorder.Event(time.Unix(3, 4), request); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stored), "## Tool call — read (1-200) · +2.0s\n\n```\n"+subject+"\n```") {
		t.Errorf("expected a call too long to read on one line to be fenced, got:\n%s", stored)
	}
}

func TestTranscriptNamesTheToolOfAResultThatFollowsAnotherCall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "brave-otter", Started: time.Unix(1, 2), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"call-1", "call-2"} {
		request := agent.Event{Kind: agent.ToolCallRequestEvent, ID: id, Name: "read"}
		request.Subject = id + ".go"
		if err := recorder.Event(time.Unix(3, 4), request); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"call-1", "call-2"} {
		if err := recorder.Event(time.Unix(5, 6), agent.Event{
			Kind:   agent.ToolCallResultEvent,
			ID:     id,
			Name:   "read",
			Status: agent.SuccessStatus,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	transcript := string(stored)
	if strings.Contains(transcript, "→") {
		t.Errorf("expected no result to be taken for the answer to the call above it, got:\n%s", transcript)
	}
	if strings.Count(transcript, "## Tool result — read · success · +4.0s") != 2 {
		t.Errorf("expected both results to name the tool they answer for, got:\n%s", transcript)
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
	if !strings.Contains(transcript, "```sh\njq -r 'select(.event.kind == \"tool_call_result\" and .event.id == \"call-1\") | .event.text' session.jsonl\n```") {
		t.Errorf("expected a command that reads the complete result, got:\n%s", transcript)
	}
}

func TestTranscriptDescribesAToolResultWithoutAnIDInProse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "brave-otter", Started: time.Unix(1, 2), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(3, 4), agent.Event{
		Kind: agent.ToolCallResultEvent,
		Name: "read",
		Text: "first\nsecond\nthird\nfourth",
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
	if strings.Contains(transcript, "jq") {
		t.Errorf("expected no command without an identifier to match on, got:\n%s", transcript)
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

func TestTranscriptOmitsDurableState(t *testing.T) {
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
	for _, unwanted := range []string{"## State", "`call`", "`file_read`", string(state)} {
		if strings.Contains(transcript, unwanted) {
			t.Errorf("expected %q to be left out of the transcript, got:\n%s", unwanted, transcript)
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
	for _, want := range []string{"## Notice · error", "no space left on device"} {
		if !strings.Contains(transcript, want) {
			t.Errorf("expected %q in the transcript, got:\n%s", want, transcript)
		}
	}
}

func TestTranscriptHoldsTheHeaderApartFromWhatFollowsIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "brave-otter", Started: time.Unix(1, 2), Workspace: "/workspace"})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(3, 4), agent.Event{
		Kind:   agent.HarnessMessageEvent,
		Status: agent.WarningStatus,
		Text:   "the turn was stopped",
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
	for _, want := range []string{
		"- **Workspace:** `/workspace`\n\n## Notice · warning · +2.0s\n",
		"## Mode · rx · +4.0s\n\n## User · +6.0s\n",
	} {
		if !strings.Contains(transcript, want) {
			t.Errorf("expected %q in the transcript, got:\n%s", want, transcript)
		}
	}
}
