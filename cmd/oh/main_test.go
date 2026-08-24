package main

import (
	"os"
	"slices"
	"testing"

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
		mode:         caps.NewMode(currentCaps),
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

	want := []string{"--workspace", "/tmp/somewhere", "--caps", "rxw"}

	if got := self.restartArguments(); !slices.Equal(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}
