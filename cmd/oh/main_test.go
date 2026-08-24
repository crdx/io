package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/cli"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/segment/modeToggle"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/internal/file"
	"crdx.org/io/internal/sandbox"
	"crdx.org/io/tool"
)

func TestMain(testingMain *testing.M) {
	sandbox.Init()
	os.Exit(testingMain.Run())
}

func TestHomeMountIsReadableByFileTools(t *testing.T) {
	workspaceRoot, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()

	files := file.New(workspaceRoot, func(string) error { return file.ErrReadOnly })
	home := t.TempDir()
	path := filepath.Join(home, "reference")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	homeRoot, err := mountHomeDir(files, home, caps.NewMode(caps.Read))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = homeRoot.Close() }()

	resolvedRoot, name, err := files.Resolve(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := resolvedRoot.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Errorf("got %q, want hello", data)
	}
}

func TestTmpMountIsWritableWithoutAShell(t *testing.T) {
	workspaceRoot, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()

	files := file.New(workspaceRoot, func(string) error { return file.ErrReadOnly })
	tmp := t.TempDir()
	tmpRoot, err := mountTmpDir(files, tmp)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tmpRoot.Close() }()

	resolvedRoot, name, err := files.Resolve("/tmp/proof")
	if err != nil {
		t.Fatal(err)
	}
	if err := resolvedRoot.WriteFile(name, []byte("written"), 0o600); err != nil {
		t.Fatalf("tmp was not writable: %v", err)
	}
}

func TestAResumedConversationOpensInTheModeItWasLeftIn(t *testing.T) {
	leftCaps := caps.Read | caps.Git
	resumedSession := &store.Session{Events: []agent.Event{caps.ModeEvent(leftCaps)}}
	assumed := cli.Options{Session: "one", Caps: caps.Read | caps.Write}

	got, err := openingCaps(assumed, resumedSession)
	if err != nil || got != leftCaps {
		t.Errorf("expected %s, got %s and %v", leftCaps.Flags(), got.Flags(), err)
	}

	got, err = openingCaps(assumed, &store.Session{})
	if err != nil || got != assumed.Caps {
		t.Errorf("expected a silent session to leave %s alone, got %s and %v", assumed.Caps.Flags(), got.Flags(), err)
	}
}

func TestAResumedConversationDrawsItsRecordedMode(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{Model: "gpt"})
	if err != nil {
		t.Fatal(err)
	}

	recordedCaps := caps.Read | caps.Shell | caps.Git
	if err := log.Event(caps.ModeEvent(recordedCaps)); err != nil {
		t.Fatal(err)
	}
	if err := log.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	storedSession, err := store.Read(directory, log.Name())
	if err != nil {
		t.Fatal(err)
	}
	restoredCaps, err := openingCaps(cli.Options{Caps: caps.Read | caps.Shell | caps.Write}, storedSession)
	if err != nil {
		t.Fatal(err)
	}

	resumedHarness := &Harness{mode: caps.NewResumedMode(restoredCaps)}
	modeSegment, err := modeToggle.New(resumedHarness.grantedCaps, resumedHarness.isChordPending)(nil)
	if err != nil {
		t.Fatal(err)
	}
	passes := map[string]func() string{
		"recorded rxg rather than default rxw": func() string {
			return modeSegment.Render(segment.Context{})
		},
	}

	compareWithGolden(t, "resume-mode", ".ansi", passes)
}

func TestAResumedConversationCannotBeAskedForAnotherMode(t *testing.T) {
	leftCaps := caps.Read | caps.Git
	resumedSession := &store.Session{Events: []agent.Event{caps.ModeEvent(leftCaps)}}

	if _, err := openingCaps(cli.Options{Session: "one", Caps: caps.Read | caps.Shell, WereCapsChosen: true}, resumedSession); err == nil {
		t.Error("expected another mode to be refused")
	}

	got, err := openingCaps(cli.Options{Session: "one", Caps: leftCaps, WereCapsChosen: true}, resumedSession)
	if err != nil || got != leftCaps {
		t.Errorf("expected the mode it was left in to be allowed, got %s and %v", got.Flags(), err)
	}
}

func conversationFixture(t *testing.T, hasSession bool, currentCaps caps.Set) *Harness {
	t.Helper()

	log, err := store.Create(t.TempDir(), store.Meta{Model: "gpt"})
	if err != nil {
		t.Fatal(err)
	}

	if hasSession {
		if err := log.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "hello"}); err != nil {
			t.Fatal(err)
		}
	}

	t.Cleanup(func() { _ = log.Close() })

	return &Harness{
		recorder:     recordSession(log),
		workspaceDir: "/tmp/somewhere",
		restartModel: "codex/gpt@high",
		mode:         caps.NewMode(currentCaps),
	}
}

func modeFixture(t *testing.T) (*Harness, string) {
	t.Helper()

	currentCaps := caps.Read | caps.Write | caps.Shell
	directory := t.TempDir()

	log, err := store.Create(directory, store.Meta{Model: "gpt"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })

	if err := log.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "hello"}); err != nil {
		t.Fatal(err)
	}

	self := &Harness{
		agent:    agent.New("", quietProvider{}, nil),
		recorder: recordSession(log),
		screen:   output.New(&bytes.Buffer{}),
		mode:     caps.NewMode(currentCaps),
	}
	self.settleMode()

	return self, directory
}

func recordedModes(t *testing.T, self *Harness, directory string) []agent.Event {
	t.Helper()

	storedSession, err := store.Read(directory, self.recorder.Name())
	if err != nil {
		t.Fatal(err)
	}

	var recorded []agent.Event
	for _, event := range storedSession.Events {
		if event.Kind == caps.ModeChange {
			recorded = append(recorded, event)
		}
	}

	return recorded
}

func TestAModeChangeIsWrittenDownOnceItSettles(t *testing.T) {
	self, directory := modeFixture(t)

	self.toggleCap(caps.Git)
	self.settleMode()

	recorded := recordedModes(t, self, directory)
	if len(recorded) != 2 {
		t.Fatalf("expected the opening mode and the change, got %v", recorded)
	}

	want := caps.Read | caps.Write | caps.Shell | caps.Git
	if got, said := caps.LastRecordedMode(recorded); !said || got != want {
		t.Errorf("expected %s, got %s and %t", want.Flags(), got.Flags(), said)
	}
}

func TestACapabilitySwappedBackIsTakenBackRatherThanWrittenDown(t *testing.T) {
	self, directory := modeFixture(t)

	self.toggleCap(caps.Git)
	if len(self.events) != 1 {
		t.Fatalf("expected the change to be shown, got %v", self.events)
	}

	self.toggleCap(caps.Git)
	if len(self.events) != 0 {
		t.Errorf("expected the change to be taken back, got %v", self.events)
	}

	self.settleMode()
	if recorded := recordedModes(t, self, directory); len(recorded) != 1 {
		t.Errorf("expected the opening mode alone, got %v", recorded)
	}
}

func TestACapabilitySwappedBackLeavesTheOtherChangesSayingWhatTheySaid(t *testing.T) {
	self, directory := modeFixture(t)

	self.toggleCap(caps.Git)
	self.toggleCap(caps.Write)

	shown, said := caps.ModeNotice(self.events[1])
	if !said {
		t.Fatal("expected the second change to say something")
	}

	self.toggleCap(caps.Git)
	if again, _ := caps.ModeNotice(self.events[0]); again != shown {
		t.Errorf("expected %q, got %q", shown, again)
	}

	self.settleMode()
	want := caps.Read | caps.Shell
	if got, _ := caps.LastRecordedMode(recordedModes(t, self, directory)); got != want {
		t.Errorf("expected %s, got %s", want.Flags(), got.Flags())
	}
}

func TestAModeChangeSaysItselfInTheScrollback(t *testing.T) {
	var screenOutput bytes.Buffer

	self, _ := modeFixture(t)
	self.screen = output.New(&screenOutput)
	self.toggleCap(caps.Git)

	if !strings.Contains(screenOutput.String(), "The .git directory is now read-write.") {
		t.Errorf("expected the change to be said, got %q", screenOutput.String())
	}
}

func TestStartingAgainNamesTheSessionAndKeepsTheMode(t *testing.T) {
	self := conversationFixture(t, true, caps.Read|caps.Write|caps.Shell)

	want := []string{"-r", self.recorder.Name(), "--caps", "rxw"}

	if got := self.restartArguments(); !slices.Equal(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func TestStartingAgainAsksForWhateverWasSwappedMidConversation(t *testing.T) {
	self := conversationFixture(t, true, caps.Read|caps.Write|caps.Shell)

	self.mode.Toggle(caps.Write)
	self.mode.Toggle(caps.Git)

	want := []string{"-r", self.recorder.Name(), "--caps", "rxg"}

	if got := self.restartArguments(); !slices.Equal(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func TestStartingAgainWithNothingStoredKeepsTheWorkspace(t *testing.T) {
	self := conversationFixture(t, false, caps.Read|caps.Write|caps.Shell)

	want := []string{
		"--workspace", "/tmp/somewhere", "--model", "codex/gpt@high", "--caps", "rxw",
	}

	if got := self.restartArguments(); !slices.Equal(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func TestStartingAgainKeepsTheEnabledTools(t *testing.T) {
	self := conversationFixture(t, true, caps.Read)
	self.enabledToolNames = []string{"read", "grep"}

	want := []string{"-r", self.recorder.Name(), "--caps", "r", "--tool", "read", "--tool", "grep"}
	if got := self.restartArguments(); !slices.Equal(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func TestOnlyNamedToolsAreEnabled(t *testing.T) {
	availableTools := []tool.Tool{slowTool("read"), slowTool("grep"), slowTool("write")}

	allTools, err := reduceTools(availableTools, nil)
	if err != nil || len(allTools) != len(availableTools) {
		t.Fatalf("expected every tool by default, got %v, %v", allTools, err)
	}

	enabledTools, err := reduceTools(availableTools, []string{"write", "read", "read"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := make([]string, 0, len(enabledTools))
	for _, enabledTool := range enabledTools {
		names = append(names, enabledTool.Name())
	}
	if !slices.Equal(names, []string{"read", "write"}) {
		t.Errorf("expected read and write in canonical order, got %v", names)
	}

	if _, err := reduceTools(availableTools, []string{"gone"}); err == nil {
		t.Error("expected an unavailable tool to be rejected")
	}
}
