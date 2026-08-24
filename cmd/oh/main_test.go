package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"crdx.org/duckopt/v2"
	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/internal/file"
	"crdx.org/io/internal/sandbox"
)

func TestMain(testingMain *testing.M) {
	sandbox.Init()
	os.Exit(testingMain.Run())
}

func bind(t *testing.T, arguments ...string) InputOpts {
	t.Helper()

	originalArgs := os.Args
	t.Cleanup(func() { os.Args = originalArgs })

	os.Args = append([]string{"oh"}, arguments...)

	bound, err := duckopt.Bind[InputOpts](usage, "$0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return *bound
}

func parseOptions(t *testing.T, arguments ...string) Opts {
	t.Helper()

	settledOptions, err := bind(t, arguments...).parse()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return settledOptions
}

func useCachedModels(t *testing.T) {
	t.Helper()

	t.Setenv("XDG_STATE_HOME", t.TempDir())

	path := modelCachePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}

	data := []byte(`{"version":1,"providers":{"opencode-go":{"models":[{"id":"deepseek-v4-pro","efforts":["high","max"],"output":384000}]}}}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestEveryOptionIsRead(t *testing.T) {
	useCachedModels(t)

	parsedOptions := parseOptions(t, "-c", "r", "-d", "somewhere", "-m", "deepseek@hi")

	if parsedOptions.caps != caps.Read {
		t.Errorf("expected reading alone, got %s", parsedOptions.caps.Flags())
	}

	if parsedOptions.workspaceDir != "somewhere" {
		t.Errorf("expected the directory, got %q", parsedOptions.workspaceDir)
	}

	if parsedOptions.provider != opencodeGoProvider || parsedOptions.model != "deepseek-v4-pro" || parsedOptions.effort != "high" {
		t.Errorf("expected opencode-go/deepseek-v4-pro@high, got %s/%s@%s", parsedOptions.provider, parsedOptions.model, parsedOptions.effort)
	}

	id := "0347juX1xcrL9W0QKJe0cs"

	if parsedOptions := parseOptions(t, "-r", id); parsedOptions.session != id || !parsedOptions.resuming() {
		t.Errorf("expected the session, got %q", parsedOptions.session)
	}
}

func TestModelSelectionRequiresModelAndEffort(t *testing.T) {
	for _, selection := range []string{"model", "model@", "@high", "model@high@extra"} {
		if _, err := (InputOpts{Model: selection}).parse(); err == nil {
			t.Errorf("expected %q to be rejected", selection)
		}
	}
}

func TestASessionMayBeResumedWithAPromptBesideIt(t *testing.T) {
	parsedOptions := parseOptions(t, "-r", "0347juX1xcrL9W0QKJe0cs", "carry", "on")

	if !parsedOptions.resuming() {
		t.Error("expected the session to be resumed")
	}

	if parsedOptions.message != "carry on" {
		t.Errorf("expected the prompt beside it, got %q", parsedOptions.message)
	}
}

func TestTheVersionIsAskedForOnItsOwn(t *testing.T) {
	for _, argument := range []string{"--version", "-V"} {
		if !bind(t, argument).Version {
			t.Errorf("expected the version to be asked for by %s", argument)
		}
	}

	if version() == "" {
		t.Error("expected a version, got nothing")
	}
}

func TestTheModelListIsAskedForOnItsOwn(t *testing.T) {
	for _, argument := range []string{"--list", "-l"} {
		if !bind(t, argument).List {
			t.Errorf("expected the model list to be asked for by %s", argument)
		}
	}
}

func TestTheSessionPickerIsAskedForOnItsOwn(t *testing.T) {
	for _, argument := range []string{"--sessions", "-s"} {
		if !bind(t, argument).Sessions {
			t.Errorf("expected the session picker to be asked for by %s", argument)
		}
	}
}

func TestWhateverIsLeftOverIsTheFirstThingSaid(t *testing.T) {
	parsedOptions := parseOptions(t, "why", "does", "the", "spinner", "stutter")

	if parsedOptions.message != "why does the spinner stutter" {
		t.Errorf("expected the words back as one, got %q", parsedOptions.message)
	}

	if parsedOptions.workspaceDir != "." {
		t.Errorf("expected the current directory, got %q", parsedOptions.workspaceDir)
	}
}

func TestTheWorkingDirectoryIsNotTakenFromThePrompt(t *testing.T) {
	parsedOptions := parseOptions(t, "read", "main.go", "-d", "/tmp")

	if parsedOptions.workspaceDir != "/tmp" {
		t.Errorf("expected the directory to come from the option, got %q", parsedOptions.workspaceDir)
	}

	if parsedOptions.message != "read main.go" {
		t.Errorf("expected the rest to be the prompt, got %q", parsedOptions.message)
	}
}

func TestTheDefaultCapabilitiesAreEverythingButTheHistory(t *testing.T) {
	parsedOptions := parseOptions(t)

	if got := parsedOptions.caps.Flags(); got != "rxw" {
		t.Errorf("expected rxw, got %q", got)
	}

	if parsedOptions.message != "" {
		t.Errorf("expected nothing said, got %q", parsedOptions.message)
	}
}

func TestCapabilitiesAreReadAsTheLettersTheyAreSpelledWith(t *testing.T) {
	for _, capString := range []string{"rwxgb", "bgxwr", "wxgb"} {
		currentCaps, err := caps.Parse(capString)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", capString, err)
		}

		if got := currentCaps.Flags(); got != caps.AllFlags {
			t.Errorf("%s: expected it written back as %s, got %q", capString, caps.AllFlags, got)
		}
	}

	if _, err := caps.Parse("rwz"); err == nil {
		t.Error("expected a letter naming no capability to be refused")
	}
}

func TestReadingIsAlwaysGranted(t *testing.T) {
	grantedCaps, err := caps.Parse("")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if got := grantedCaps.Flags(); got != "r" {
		t.Errorf("expected r, got %q", got)
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

func TestAWorkspaceCannotBeGivenWhenResuming(t *testing.T) {
	opts := InputOpts{Session: "one", WorkspaceDir: "somewhere"}
	if _, err := opts.parse(); err == nil {
		t.Error("expected an error")
	}
}

func TestAResumedConversationMayBeGrantedSomethingElse(t *testing.T) {
	opts := InputOpts{Session: "one", Caps: "rx"}
	settledOptions, err := opts.parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if settledOptions.caps.Has(caps.Write) {
		t.Error("expected writing to be held back")
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
		log:          log,
		workspaceDir: "/tmp/somewhere",
		mode:         caps.NewMode(currentCaps),
	}
}

func TestStartingAgainNamesTheSessionAndKeepsTheMode(t *testing.T) {
	self := conversationFixture(t, true, caps.Read|caps.Write|caps.Shell)

	want := []string{"-r", self.log.Name(), "--caps", "rxw"}

	if got := self.restartArguments(); !slices.Equal(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func TestStartingAgainAsksForWhateverWasSwappedMidConversation(t *testing.T) {
	self := conversationFixture(t, true, caps.Read|caps.Write|caps.Shell)

	self.mode.Toggle(caps.Write)
	self.mode.Toggle(caps.Git)

	want := []string{"-r", self.log.Name(), "--caps", "rxg"}

	if got := self.restartArguments(); !slices.Equal(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func TestStartingAgainWithNothingStoredKeepsTheWorkspace(t *testing.T) {
	self := conversationFixture(t, false, caps.Read|caps.Write|caps.Shell)

	want := []string{"--workspace", "/tmp/somewhere", "--caps", "rxw"}

	if got := self.restartArguments(); !slices.Equal(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}
