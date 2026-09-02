package transcript_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/pathgrant"
	"crdx.org/io/cmd/oh/store/transcript"
	"crdx.org/io/tool"
)

func TestTranscriptOmitsReasoningEntirely(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "brave-otter", StartedAt: time.Unix(1, 2), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(3, 4), agent.Event{Kind: agent.ModelReasoningEvent, Text: "First. Second?\nThird!"}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(5, 6), agent.Event{Kind: agent.ModelMessageEvent, Text: "answer"}); err != nil {
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
	if strings.Contains(transcript, "Reasoning") || strings.Contains(transcript, "First. Second?") || strings.Contains(transcript, "Third!") {
		t.Errorf("expected no trace of reasoning at all, got:\n%s", transcript)
	}
	if !strings.Contains(transcript, "## Assistant · +4.0s\n\nanswer\n") {
		t.Errorf("expected the reasoning to leave the next event untouched, got:\n%s", transcript)
	}
}

func TestTranscriptRoundsShortElapsedTimesToTenths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	startedAt := time.Unix(1, 0)
	recorder, err := transcript.Open(path, transcript.Meta{Name: "brave-otter", StartedAt: startedAt, Model: "model"})
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

func TestTranscriptLogsACallAndItsResultOnOneLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "brave-otter", StartedAt: time.Unix(1, 2), Model: "model"})
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
			Kind:        tool.StatsResources,
			Lines:       1,
			Bytes:       2048,
			TotalBytes:  4096,
			CPUTime:     2500 * time.Millisecond,
			PeakMemory:  4 << 20,
			IsTruncated: true,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(7, 8), agent.Event{Kind: agent.ModelMessageEvent, Text: "done"}); err != nil {
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
	want := "## Tool calls · +2.0s\n\n```\nbash: ls [ok in 12s, 1 line, 2K of 4K, 2.5s CPU, 4M peak, truncated, call-1]\n```"
	if !strings.Contains(transcript, want) {
		t.Errorf("expected one heading naming the call and its result folded onto one line, got:\n%s", transcript)
	}
	if strings.Contains(transcript, `{"command":"ls"}`) || strings.Contains(transcript, "## Tool call —") || strings.Contains(transcript, "## Tool result —") {
		t.Errorf("expected no per-call heading and no raw arguments, got:\n%s", transcript)
	}
}

func TestTranscriptDoesNotAttributeToolCallsToTheUser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "brave-otter", StartedAt: time.Unix(1, 2), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(3, 4), agent.Event{Kind: agent.UserMessageEvent, Text: "check web"}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(5, 6), agent.Event{Kind: agent.ToolCallRequestEvent, ID: "call-1", Name: "web_search"}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(7, 8), agent.Event{
		Kind: agent.ToolCallResultEvent, ID: "call-1", Name: "web_search", Status: agent.SuccessStatus,
	}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(9, 10), agent.Event{Kind: agent.ModelMessageEvent, Text: "done"}); err != nil {
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
	want := "## User · +2.0s\n\ncheck web\n\n## Tool calls · +4.0s\n\n```\nweb_search [ok, call-1]\n```\n\n## Assistant · +8.0s"
	if !strings.Contains(transcript, want) {
		t.Errorf("expected the call log held under its own heading between the user and the answer, got:\n%s", transcript)
	}
}

func TestTranscriptLogsSeveralCallsInOneFencedBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "brave-otter", StartedAt: time.Unix(1, 2), Model: "model"})
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
	want := "```\nread: call-1.go [ok, call-1]\nread: call-2.go [ok, call-2]\n```"
	if !strings.Contains(transcript, want) {
		t.Errorf("expected both calls folded into one block, in order, got:\n%s", transcript)
	}
	if strings.Count(transcript, "```") != 2 {
		t.Errorf("expected exactly one fenced block for both calls, got:\n%s", transcript)
	}
}

func TestTranscriptFallsBackToTheArgumentsOfAnUnrenderedCall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "brave-otter", StartedAt: time.Unix(1, 2), Model: "model"})
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
	transcript := string(stored)
	if !strings.Contains(transcript, "```\nsummon [call-1]\n```") {
		t.Errorf("expected an unanswered call to be logged with what is known, got:\n%s", transcript)
	}
	if strings.Contains(transcript, `{"name":"one"}`) {
		t.Errorf("expected the raw arguments to give way to the call log, got:\n%s", transcript)
	}
}

func TestTranscriptCollapsesAndTruncatesALongSubject(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "brave-otter", StartedAt: time.Unix(1, 2), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	request := agent.Event{Kind: agent.ToolCallRequestEvent, ID: "call-1", Name: "bash"}
	request.Subject = "for line in\n  one\n  two\ndo\n  " + strings.Repeat("echo hi; ", 20) + "\ndone"
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
	transcript := string(stored)
	start := strings.Index(transcript, "```\n") + len("```\n")
	end := strings.Index(transcript[start:], "\n```")
	block := transcript[start : start+end]
	if strings.Contains(block, "\n") {
		t.Errorf("expected the subject to collapse onto one line, got:\n%s", block)
	}
	if !strings.Contains(block, "…") {
		t.Errorf("expected a long subject to be truncated, got:\n%s", block)
	}
}

func TestTranscriptLogsACallWithNoSubject(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "brave-otter", StartedAt: time.Unix(1, 2), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(3, 4), agent.Event{Kind: agent.ToolCallRequestEvent, ID: "call-1", Name: "ls"}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(5, 6), agent.Event{
		Kind: agent.ToolCallResultEvent, ID: "call-1", Name: "ls", Status: agent.SuccessStatus,
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
	if !strings.Contains(string(stored), "```\nls [ok, call-1]\n```") {
		t.Errorf("expected no colon where there is no subject to name, got:\n%s", stored)
	}
}

func TestTranscriptLogsAResultWithoutAMatchingCallInTheSameRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "brave-otter", StartedAt: time.Unix(1, 2), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(3, 4), agent.Event{
		Kind: agent.ToolCallResultEvent, ID: "call-1", Name: "read", Status: agent.SuccessStatus,
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
	if !strings.Contains(string(stored), "```\nread [ok, call-1]\n```") {
		t.Errorf("expected a stray result to still be logged by name, got:\n%s", stored)
	}
}

func TestTranscriptWritesMessagesAsMarkdownRatherThanFencingThem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "brave-otter", StartedAt: time.Unix(1, 2), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	content := "**bold**, a [link](https://example.com), and:\n\n```go\nfunc main() {}\n```"
	if err := recorder.Event(time.Unix(3, 4), agent.Event{Kind: agent.UserMessageEvent, Text: "please explain"}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(5, 6), agent.Event{Kind: agent.ModelMessageEvent, Text: content}); err != nil {
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
	if !strings.Contains(transcript, "## User · +2.0s\n\nplease explain\n") {
		t.Errorf("expected the user's message written as plain markdown, got:\n%s", transcript)
	}
	if !strings.Contains(transcript, "## Assistant · +4.0s\n\n"+content+"\n") {
		t.Errorf("expected the answer written as its own markdown rather than fenced, got:\n%s", transcript)
	}
}

func TestTranscriptUsesAFenceLongerThanItsContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "brave-otter", StartedAt: time.Unix(1, 2), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	content := "before\n````\nafter"
	if err := recorder.Event(time.Unix(3, 4), agent.Event{Kind: agent.FailureEvent, Text: content}); err != nil {
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
	if !strings.Contains(transcript, "# Conversation") || !strings.Contains(transcript, "## Failure") {
		t.Errorf("expected the metadata and event headings, got:\n%s", transcript)
	}
}

func TestTranscriptRetainsTurnFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "brave-otter", StartedAt: time.Unix(1, 2)})
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
			recorder, err := transcript.Open(path, transcript.Meta{Name: "brave-otter", StartedAt: time.Unix(1, 2)})
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
	recorder, err := transcript.Open(path, transcript.Meta{Name: "brave-otter", StartedAt: time.Unix(1, 2)})
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
	recorder, err := transcript.Open(path, transcript.Meta{Name: "brave-otter", StartedAt: time.Unix(1, 2)})
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

func TestTranscriptRendersPathGrantEventsFromStructuredState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "brave-otter", StartedAt: time.Unix(1, 2)})
	if err != nil {
		t.Fatal(err)
	}
	event, err := pathgrant.ChangeEvent("/reference", []pathgrant.Grant{{
		Path:   "/reference",
		Access: pathgrant.ReadAccess | pathgrant.WriteAccess,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(3, 4), event); err != nil {
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
		"## Path grant · 1 path · changed /reference · +2.0s",
		"Granted temporary read and write access to /reference. Changes there follow the workspace write capability.",
	} {
		if !strings.Contains(transcript, want) {
			t.Errorf("expected %q in the transcript, got:\n%s", want, transcript)
		}
	}
}

func TestTranscriptHoldsTheHeaderApartFromWhatFollowsIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "brave-otter", StartedAt: time.Unix(1, 2), Workspace: "/workspace"})
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
		"- **Workspace:** `/workspace`\n- **Tool detail:** `jq 'select(.event.id == \"<id>\")' session.jsonl`, for the `[id]` of any call\n\n## Notice · warning · +2.0s\n",
		"## Mode · rx · +4.0s\n\n## User · +6.0s\n",
	} {
		if !strings.Contains(transcript, want) {
			t.Errorf("expected %q in the transcript, got:\n%s", want, transcript)
		}
	}
}
