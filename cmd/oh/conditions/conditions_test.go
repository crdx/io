package conditions

import (
	"strings"
	"testing"

	"crdx.org/io/agent"
)

func TestUnchangedConditionsSayNothing(t *testing.T) {
	current := Conditions{UnixSockets: true, IPv6: true, Interactive: true}
	state := NewRestored(current, current)

	if notice := state.Peek(); notice != "" {
		t.Errorf("got %q, want nothing to say", notice)
	}
}

func TestEachLostFacilityIsToldOnce(t *testing.T) {
	known := Conditions{UnixSockets: true, IPv6: true, Interactive: true}
	current := Conditions{}
	state := NewRestored(current, known)

	notice := state.Inject()
	for _, want := range []string{
		"A Unix socket no longer works at any path",
		"This machine has lost IPv6",
		"The user has gone",
	} {
		if !strings.Contains(notice, want) {
			t.Errorf("notice %q does not contain %q", notice, want)
		}
	}

	if repeated := state.Inject(); repeated != "" {
		t.Errorf("got %q, want the change to be told only once", repeated)
	}
}

func TestEachRegainedFacilityIsTold(t *testing.T) {
	state := NewRestored(Conditions{UnixSockets: true, IPv6: true, Interactive: true}, Conditions{})

	notice := state.Peek()
	for _, want := range []string{
		"A Unix socket now works beneath /tmp",
		"This machine has IPv6 again",
		"The user has returned",
	} {
		if !strings.Contains(notice, want) {
			t.Errorf("notice %q does not contain %q", notice, want)
		}
	}
}

func TestAChangeIsRecordedAndReadBack(t *testing.T) {
	known := Conditions{UnixSockets: true, IPv6: true, Interactive: true}
	current := Conditions{Interactive: true}

	event, err := ChangeEvent(known, current)
	if err != nil {
		t.Fatal(err)
	}

	notice, isSaid := Notice(event)
	if !isSaid {
		t.Fatal("a changed condition says nothing")
	}
	if want := "A Unix socket no longer works at any path"; !strings.Contains(notice, want) {
		t.Errorf("notice %q does not contain %q", notice, want)
	}

	recorded, found := LastRecorded([]agent.Event{event})
	if !found {
		t.Fatal("the recorded conditions were not found")
	}
	if recorded != current {
		t.Errorf("got %+v, want %+v", recorded, current)
	}
}

func TestABaselineIsRecordedWithoutSayingAnything(t *testing.T) {
	current := Conditions{UnixSockets: true, IPv6: true, Interactive: true}

	event, err := ChangeEvent(current, current)
	if err != nil {
		t.Fatal(err)
	}

	if notice, isSaid := Notice(event); isSaid {
		t.Errorf("a baseline says %q", notice)
	}

	recorded, found := LastRecorded([]agent.Event{event})
	if !found {
		t.Fatal("the recorded conditions were not found")
	}
	if recorded != current {
		t.Errorf("got %+v, want %+v", recorded, current)
	}
}

func TestAnUnreadableRecordIsNotFound(t *testing.T) {
	if _, found := LastRecorded([]agent.Event{{Kind: Change, State: []byte("{")}}); found {
		t.Error("an unreadable record was found")
	}
}

func TestAWaivedSandboxAlwaysReachesUnixSockets(t *testing.T) {
	if !Probe(true, true).UnixSockets {
		t.Error("an unconfined session cannot reach a Unix socket")
	}
}

func TestASessionWithNothingRecordedIsLeftAlone(t *testing.T) {
	current := Conditions{UnixSockets: true, IPv6: true, Interactive: true}

	restored, err := Restore(nil, nil, current)
	if err != nil {
		t.Fatal(err)
	}

	if restored.IsChanged {
		t.Error("a session with nothing recorded reports a change")
	}
	if notice := restored.State.Peek(); notice != "" {
		t.Errorf("got %q, want nothing to say", notice)
	}
}

func TestTheConditionsAtCreationAreCompared(t *testing.T) {
	createdConditions := Conditions{UnixSockets: true, IPv6: true, Interactive: true}

	restored, err := Restore(&createdConditions, nil, Conditions{Interactive: true})
	if err != nil {
		t.Fatal(err)
	}

	if !restored.IsChanged {
		t.Fatal("a machine that lost a facility reports no change")
	}
	if want := "A Unix socket no longer works"; !strings.Contains(restored.State.Peek(), want) {
		t.Errorf("notice %q does not contain %q", restored.State.Peek(), want)
	}
}

func TestARecordedChangeOutranksTheConditionsAtCreation(t *testing.T) {
	createdConditions := Conditions{UnixSockets: true, IPv6: true, Interactive: true}
	current := Conditions{Interactive: true}

	recorded, err := ChangeEvent(createdConditions, current)
	if err != nil {
		t.Fatal(err)
	}

	restored, err := Restore(&createdConditions, []agent.Event{recorded}, current)
	if err != nil {
		t.Fatal(err)
	}

	if restored.IsChanged {
		t.Error("a change already recorded is reported a second time")
	}
	if notice := restored.State.Peek(); notice != "" {
		t.Errorf("got %q, want the change to be told only once across resumes", notice)
	}
}

func TestAMachineThatRecoversIsToldAgain(t *testing.T) {
	createdConditions := Conditions{UnixSockets: true, IPv6: true, Interactive: true}
	lost, err := ChangeEvent(createdConditions, Conditions{Interactive: true})
	if err != nil {
		t.Fatal(err)
	}

	restored, err := Restore(&createdConditions, []agent.Event{lost}, createdConditions)
	if err != nil {
		t.Fatal(err)
	}

	if !restored.IsChanged {
		t.Fatal("a recovered machine reports no change")
	}

	notice := restored.State.Peek()
	for _, want := range []string{"A Unix socket now works beneath /tmp", "This machine has IPv6 again"} {
		if !strings.Contains(notice, want) {
			t.Errorf("notice %q does not contain %q", notice, want)
		}
	}
}

func TestAnUnreadableRecordFallsBackToCreation(t *testing.T) {
	createdConditions := Conditions{UnixSockets: true, IPv6: true, Interactive: true}

	restored, err := Restore(
		&createdConditions,
		[]agent.Event{{Kind: Change, State: []byte("{")}},
		createdConditions,
	)
	if err != nil {
		t.Fatal(err)
	}

	if restored.IsChanged {
		t.Error("an unreadable record reports a change against the conditions at creation")
	}
}

func TestOnlyTheFacilitiesThatMovedAreTold(t *testing.T) {
	createdConditions := Conditions{UnixSockets: true, IPv6: true, Interactive: true}

	restored, err := Restore(&createdConditions, nil, Conditions{UnixSockets: true, Interactive: true})
	if err != nil {
		t.Fatal(err)
	}

	notice := restored.State.Peek()
	if want := "This machine has lost IPv6"; !strings.Contains(notice, want) {
		t.Errorf("notice %q does not contain %q", notice, want)
	}
	for _, unwanted := range []string{"Unix socket", "The user"} {
		if strings.Contains(notice, unwanted) {
			t.Errorf("notice %q mentions unmoved %q", notice, unwanted)
		}
	}
}

func TestASummaryNamesOnlyWhatMoved(t *testing.T) {
	event, err := ChangeEvent(
		Conditions{UnixSockets: true, IPv6: true, Interactive: true},
		Conditions{UnixSockets: true},
	)
	if err != nil {
		t.Fatal(err)
	}

	summary, isSaid := Summary(event)
	if !isSaid {
		t.Fatal("a changed condition has no summary")
	}
	if want := "ipv6, asking the user"; summary != want {
		t.Errorf("got %q, want %q", summary, want)
	}
}

func TestABaselineHasNoSummary(t *testing.T) {
	current := Conditions{UnixSockets: true}
	event, err := ChangeEvent(current, current)
	if err != nil {
		t.Fatal(err)
	}

	if summary, isSaid := Summary(event); isSaid {
		t.Errorf("a baseline summarises as %q", summary)
	}
}

func TestAnotherKindIsNeitherNoticedNorSummarised(t *testing.T) {
	event := agent.Event{Kind: agent.UserMessageEvent, Text: "hello"}

	if _, isSaid := Notice(event); isSaid {
		t.Error("another kind produced a notice")
	}
	if _, isSaid := Summary(event); isSaid {
		t.Error("another kind produced a summary")
	}
}

func TestAnUnreadableRecordIsNeitherNoticedNorSummarised(t *testing.T) {
	event := agent.Event{Kind: Change, State: []byte("{")}

	if _, isSaid := Notice(event); isSaid {
		t.Error("an unreadable record produced a notice")
	}
	if _, isSaid := Summary(event); isSaid {
		t.Error("an unreadable record produced a summary")
	}
}
