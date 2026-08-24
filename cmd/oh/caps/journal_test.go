package caps

import (
	"testing"

	"crdx.org/io/agent"
)

func TestARecordedModeIsReadBackAsItWasWritten(t *testing.T) {
	for _, grantedCaps := range []Set{Read, Read | Write, Read | Shell | Write | Git | Background} {
		event := ModeEvent(grantedCaps)

		if event.Kind != ModeChange {
			t.Errorf("expected a mode change, got %q", event.Kind)
		}

		readBack, recorded := LastRecordedMode([]agent.Event{event})
		if !recorded || readBack != grantedCaps {
			t.Errorf("expected %s, got %s and %t", grantedCaps.Flags(), readBack.Flags(), recorded)
		}
	}
}

func TestTheLastRecordedModeIsTheOneThatCounts(t *testing.T) {
	events := []agent.Event{
		ModeEvent(Read | Write),
		{Kind: agent.UserMessageEvent, Text: "hello"},
		ModeEvent(Read | Shell),
		{Kind: agent.ModelMessageEvent, Text: "hi"},
	}

	if readBack, recorded := LastRecordedMode(events); !recorded || readBack != Read|Shell {
		t.Errorf("expected rx, got %s and %t", readBack.Flags(), recorded)
	}
}

func TestAConversationThatNeverSaidItsModeSaysSo(t *testing.T) {
	events := []agent.Event{
		{Kind: agent.UserMessageEvent, Text: "hello"},
		{Kind: ModeChange, Text: "z"},
	}

	if readBack, recorded := LastRecordedMode(events); recorded || readBack != 0 {
		t.Errorf("expected nothing to be found, got %s and %t", readBack.Flags(), recorded)
	}
}

func TestAModeChangeSaysWhatItSwapped(t *testing.T) {
	notice, said := ModeNotice(ModeToggleEvent(Git, Read|Write|Git))
	if !said || notice != historyIs(true) {
		t.Errorf("expected %q, got %q and %t", historyIs(true), notice, said)
	}

	notice, said = ModeNotice(ModeToggleEvent(Write, Read))
	if !said || notice != workspaceIs(false) {
		t.Errorf("expected %q, got %q and %t", workspaceIs(false), notice, said)
	}

	if notice, said := ModeNotice(ModeEvent(Read | Write)); said {
		t.Errorf("expected the opening mode to say nothing, got %q", notice)
	}
}

func TestAChangeTakenBackLeavesTheOnesAfterItSayingWhatTheySaid(t *testing.T) {
	event := ModeToggleEvent(Write, Read|Git)
	said, _ := ModeNotice(event)

	event = ModeWithout(event, Git)

	if again, _ := ModeNotice(event); again != said {
		t.Errorf("expected %q, got %q", said, again)
	}

	if grantedCaps, _ := LastRecordedMode([]agent.Event{event}); grantedCaps != Read {
		t.Errorf("expected r, got %s", grantedCaps.Flags())
	}
}
