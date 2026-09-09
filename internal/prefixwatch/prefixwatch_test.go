package prefixwatch

import (
	"encoding/json"
	"testing"
)

func turn(role string, text string) json.RawMessage {
	return json.RawMessage(`{"role":"` + role + `","content":[{"type":"text","text":"` + text + `"}]}`)
}

func marked(role string, text string) json.RawMessage {
	return json.RawMessage(
		`{"role":"` + role + `","content":[{"cache_control":{"type":"ephemeral"},"type":"text","text":"` + text + `"}]}`,
	)
}

func TestTheFirstRequestOfAConversationHasNothingToCompare(t *testing.T) {
	var watcher Watcher
	if what := watcher.Look([]string{"read"}, "be brief", []json.RawMessage{turn("user", "go on")}); what != "" {
		t.Errorf("got %q", what)
	}
}

func TestAConversationThatOnlyGrowsIsLeftAlone(t *testing.T) {
	var watcher Watcher
	held := []json.RawMessage{turn("user", "go on"), turn("assistant", "done")}
	watcher.Look([]string{"read"}, "be brief", held)

	grown := append(append([]json.RawMessage{}, held...), turn("user", "again"), turn("assistant", "done again"))
	if what := watcher.Look([]string{"read"}, "be brief", grown); what != "" {
		t.Errorf("got %q", what)
	}
}

func TestABreakpointThatMovesIsNotARewrite(t *testing.T) {
	var watcher Watcher
	watcher.Look([]string{"read"}, "be brief", []json.RawMessage{marked("user", "go on"), turn("assistant", "done")})

	moved := []json.RawMessage{turn("user", "go on"), marked("assistant", "done"), turn("user", "again")}
	if what := watcher.Look([]string{"read"}, "be brief", moved); what != "" {
		t.Errorf("got %q", what)
	}
}

func TestATailTheModelHasNotAnsweredMayStillGrow(t *testing.T) {
	var watcher Watcher
	watcher.Look([]string{"read"}, "be brief", []json.RawMessage{turn("user", "go on")})

	if what := watcher.Look([]string{"read"}, "be brief", []json.RawMessage{turn("user", "go on and again")}); what != "" {
		t.Errorf("got %q", what)
	}
}

func TestEveryWayOfRewritingWhatWasSentIsNamed(t *testing.T) {
	held := []json.RawMessage{turn("user", "go on"), turn("assistant", "done"), turn("user", "again")}

	for name, test := range map[string]struct {
		tools  []string
		system string
		turns  []json.RawMessage
		want   string
	}{
		"a tool appears": {
			tools: []string{"read", "job"}, system: "be brief", turns: held, want: ToolsChanged,
		},
		"the system prompt gains a block": {
			tools: []string{"read"}, system: "be brief, and careful", turns: held, want: SystemChanged,
		},
		"an answered turn is rewritten": {
			tools: []string{"read"}, system: "be brief",
			turns: []json.RawMessage{turn("user", "go on differently"), turn("assistant", "done"), turn("user", "again")},
			want:  TurnChanged + " (1 of 2)",
		},
		"an answered turn is fused into another": {
			tools: []string{"read"}, system: "be brief",
			turns: []json.RawMessage{turn("user", "go on"), turn("assistant", "done and more")},
			want:  TurnChanged + " (2 of 2)",
		},
		"the conversation is cut short": {
			tools: []string{"read"}, system: "be brief",
			turns: []json.RawMessage{turn("user", "go on")},
			want:  TurnsDropped,
		},
	} {
		t.Run(name, func(t *testing.T) {
			var watcher Watcher
			watcher.Look([]string{"read"}, "be brief", held)

			if what := watcher.Look(test.tools, test.system, test.turns); what != test.want {
				t.Errorf("got %q, want %q", what, test.want)
			}
		})
	}
}
