package migrate

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/model"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/session"
)

func storedFastModeSession(t *testing.T, directory string, providerName string, events ...agent.Event) string {
	t.Helper()

	encodedMeta, err := json.Marshal(store.Meta{Provider: providerName, Model: "model", Effort: "high"})
	if err != nil {
		t.Fatal(err)
	}
	writer, err := session.Create(directory, encodedMeta, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Event(caps.ModeEvent(caps.Read)); err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if _, err := writer.Event(event); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.CompleteTurn(); err != nil {
		t.Fatal(err)
	}
	name := writer.Name()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestLegacyCodexFastModeIsBackfilledOnce(t *testing.T) {
	directory := t.TempDir()
	name := storedFastModeSession(t, directory, model.CodexProvider)
	options := Options{Directory: directory, BackupDir: filepath.Join(t.TempDir(), "copies")}

	wasBackfilled, err := backfillFastMode(options, name)
	if err != nil || !wasBackfilled {
		t.Fatalf("got backfilled=%t error=%v", wasBackfilled, err)
	}
	storedSession, err := store.Read(directory, name)
	if err != nil {
		t.Fatal(err)
	}
	isFast, isFound := model.LastRecordedFastMode(storedSession.Events)
	if !isFound || !isFast {
		t.Errorf("got fast=%t found=%t", isFast, isFound)
	}
	if len(storedSession.Events) < 2 || storedSession.Events[0].Kind != caps.ModeChange ||
		storedSession.Events[1].Name != model.FastModeStateKey {
		t.Errorf("got events %+v", storedSession.Events)
	}

	wasBackfilled, err = backfillFastMode(options, name)
	if err != nil || wasBackfilled {
		t.Errorf("second run got backfilled=%t error=%v", wasBackfilled, err)
	}
}

func TestRecordedFastModeAndOtherProvidersAreUntouched(t *testing.T) {
	for name, test := range map[string]struct {
		providerName string
		events       []agent.Event
	}{
		"standard codex": {providerName: model.CodexProvider, events: []agent.Event{model.FastModeEvent(false)}},
		"fast codex":     {providerName: model.CodexProvider, events: []agent.Event{model.FastModeEvent(true)}},
		"anthropic":      {providerName: model.AnthropicProvider},
	} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			sessionName := storedFastModeSession(t, directory, test.providerName, test.events...)
			wasBackfilled, err := backfillFastMode(
				Options{Directory: directory, BackupDir: filepath.Join(t.TempDir(), "copies")},
				sessionName,
			)
			if err != nil || wasBackfilled {
				t.Errorf("got backfilled=%t error=%v", wasBackfilled, err)
			}
		})
	}
}

func TestFastModeBackfillDryRunChangesNothing(t *testing.T) {
	directory := t.TempDir()
	name := storedFastModeSession(t, directory, model.CodexProvider)

	wasBackfilled, err := backfillFastMode(Options{Directory: directory, DryRun: true}, name)
	if err != nil || !wasBackfilled {
		t.Fatalf("got backfilled=%t error=%v", wasBackfilled, err)
	}
	storedSession, err := store.Read(directory, name)
	if err != nil {
		t.Fatal(err)
	}
	if _, isFound := model.LastRecordedFastMode(storedSession.Events); isFound {
		t.Error("dry run changed the journal")
	}
}
