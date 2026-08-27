package call_test

import (
	"testing"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/call"
	"crdx.org/io/cmd/oh/style"
)

func label() call.Label {
	return call.Label{Name: "grep", Subject: "hello", Qualifier: "in internal"}
}

func check(t *testing.T, room int, name string, subject string, qualifier string) {
	t.Helper()

	elidedLabel, _ := label().Elide(room).(call.Label)

	if elidedLabel.Name != name || elidedLabel.Subject != subject || elidedLabel.Qualifier != qualifier {
		t.Errorf(
			"in %d columns expected %q %q %q, got %q %q %q",
			room, name, subject, qualifier, elidedLabel.Name, elidedLabel.Subject, elidedLabel.Qualifier,
		)
	}
}

func TestALabelThatFitsIsLeftAlone(t *testing.T) {
	check(t, 22, "grep", "hello", "in internal")
	check(t, 80, "grep", "hello", "in internal")
}

func TestWhatQualifiesTheArgumentsIsCutFirst(t *testing.T) {
	check(t, 21, "grep", "hello", "in intern…")
	check(t, 12, "grep", "hello", "…")
}

func TestWhatQualifiesTheArgumentsGoesBeforeTheArgumentsAreCut(t *testing.T) {
	check(t, 10, "grep", "hello", "")
	check(t, 9, "grep", "hel…", "")
}

func TestTheNameIsTheLastToGo(t *testing.T) {
	check(t, 5, "grep", "", "")
	check(t, 3, "gr…", "", "")
	check(t, 1, "…", "", "")
	check(t, 0, "", "", "")
}

func TestALabelWithNothingQualifyingItIsUnaffected(t *testing.T) {
	elidedLabel, _ := call.Label{Name: "ls", Subject: "internal"}.Elide(80).(call.Label)

	if elidedLabel.Name != "ls" || elidedLabel.Subject != "internal" || elidedLabel.Qualifier != "" {
		t.Errorf("expected the label to stand, got %q %q %q", elidedLabel.Name, elidedLabel.Subject, elidedLabel.Qualifier)
	}
}

func TestALabelIsCutToTheCellsItHasRatherThanTheCharacters(t *testing.T) {
	elidedLabel, _ := call.Label{Name: "read", Subject: "日本語です"}.Elide(11).(call.Label)

	if elidedLabel.Name != "read" {
		t.Errorf("expected the name to survive, got %q", elidedLabel.Name)
	}

	if elidedLabel.Subject != "日本…" {
		t.Errorf("expected two characters and an ellipsis, got %q", elidedLabel.Subject)
	}

	if got := style.Width(elidedLabel.Name + " " + elidedLabel.Subject); got != 10 {
		t.Errorf("expected the label to measure 10 cells, got %d", got)
	}
}

func TestCallNamesAreDrawnFromTheTable(t *testing.T) {
	for name, test := range map[string]struct {
		eventName string
		want      call.Label
	}{
		"shell":      {eventName: "bash", want: call.Label{Name: "$", NameStyle: style.Shell}},
		"web search": {eventName: "web_search", want: call.Label{Name: "search", NameStyle: style.Network}},
		"web fetch":  {eventName: "web_fetch", want: call.Label{Name: "fetch", NameStyle: style.Network}},
		"ordinary":   {eventName: "grep", want: call.Label{Name: "grep"}},
	} {
		t.Run(name, func(t *testing.T) {
			label := call.LabelFor(agent.Event{Name: test.eventName}, nil, "")

			if got, want := label.Render(), test.want.Render(); got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

func TestOnlyAShellCommandOrAFailureSaysWhatItReturned(t *testing.T) {
	for name, test := range map[string]struct {
		event agent.Event
		want  string
	}{
		"shell": {
			event: agent.Event{Name: "bash", Status: agent.SuccessStatus, Text: "hello"},
			want:  "hello",
		},
		"failure": {
			event: agent.Event{Name: "read", Status: agent.ErrorStatus, Text: "no such file"},
			want:  "no such file",
		},
		"ordinary": {
			event: agent.Event{Name: "read", Status: agent.SuccessStatus, Text: "package one"},
			want:  "",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := call.Summary(test.event); got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestASkillReadIsDrawnAsALoad(t *testing.T) {
	label := call.LabelFor(agent.Event{
		Name:              "read",
		FallbackRendering: agent.FallbackRendering{Subject: "/skills/guard-basics/SKILL.md"},
	}, nil, "")

	want := call.Label{
		Name:        "load",
		Subject:     "/skills/guard-basics/SKILL.md",
		NameStyle:   style.Skill,
		Accent:      "guard-basics",
		AccentStyle: style.Skill,
	}

	if got := label.Render(); got != want.Render() {
		t.Errorf("got %q, want %q", got, want.Render())
	}
}
