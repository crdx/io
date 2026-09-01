package session_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"crdx.org/io/session"
)

func archivedSession(t *testing.T) (string, string) {
	t.Helper()

	directory := t.TempDir()
	writer := storedSession(t, directory)
	name := writer.Name()

	if err := writer.CompleteTurn(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	return directory, name
}

func TestAnArchivedSessionIsRestoredWithEverythingItHeld(t *testing.T) {
	directory, name := archivedSession(t)

	nested := filepath.Join(session.Dir(directory, name), "notes")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "chat.md"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	before, err := session.Read(directory, name)
	if err != nil {
		t.Fatal(err)
	}

	if err := session.Archive(directory, name); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(session.Dir(directory, name)); !os.IsNotExist(err) {
		t.Errorf("expected the session directory to be gone, got %v", err)
	}
	if !session.IsArchived(directory, name) {
		t.Error("expected the session to be reported as archived")
	}

	names, err := session.ArchivedNames(directory)
	if err != nil || len(names) != 1 || names[0] != name {
		t.Fatalf("got archived names %v and %v", names, err)
	}

	if err := session.Restore(directory, name); err != nil {
		t.Fatal(err)
	}
	if session.IsArchived(directory, name) {
		t.Error("expected the archive to be gone once restored")
	}

	after, err := session.Read(directory, name)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Events) != len(before.Events) || after.ID != before.ID {
		t.Errorf("the restored session differs: %d events against %d", len(after.Events), len(before.Events))
	}

	restoredNote, err := os.ReadFile(filepath.Join(nested, "chat.md")) //nolint:gosec // a path below the test directory
	if err != nil || string(restoredNote) != "hello" {
		t.Errorf("got the nested file back as %q and %v", restoredNote, err)
	}
}

func TestAnArchivedSessionIsNoLongerListedButKeepsItsMetadata(t *testing.T) {
	directory, name := archivedSession(t)

	if err := session.Archive(directory, name); err != nil {
		t.Fatal(err)
	}

	entries, err := session.Entries(directory)
	if err != nil || len(entries) != 0 {
		t.Fatalf("got entries %v and %v", entries, err)
	}

	meta, err := session.ArchivedMeta(directory, name)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Name != name || meta.Messages != 1 {
		t.Errorf("got archived metadata %#v", meta)
	}
}

func TestAnArchivedSessionSaysSoWhenSomethingOpensIt(t *testing.T) {
	directory, name := archivedSession(t)

	if err := session.Archive(directory, name); err != nil {
		t.Fatal(err)
	}

	if _, err := session.Read(directory, name); !errors.Is(err, session.ErrArchived) {
		t.Errorf("expected the read to report an archived session, got %v", err)
	}
	if err := session.Archive(directory, name); !errors.Is(err, session.ErrArchived) {
		t.Errorf("expected a second archiving to be refused, got %v", err)
	}
}

func TestARunningSessionCannotBeArchived(t *testing.T) {
	directory := t.TempDir()
	writer := storedSession(t, directory)
	defer func() { _ = writer.Close() }()

	if err := session.Archive(directory, writer.Name()); !errors.Is(err, session.ErrInUse) {
		t.Errorf("expected the open session to be refused, got %v", err)
	}
	if session.IsArchived(directory, writer.Name()) {
		t.Error("expected nothing to have been archived")
	}
}

func TestRestoringSomethingNeverArchivedIsRefused(t *testing.T) {
	directory, name := archivedSession(t)

	if err := session.Restore(directory, name); !errors.Is(err, session.ErrNotArchived) {
		t.Errorf("expected the restore to be refused, got %v", err)
	}

	if err := session.Archive(directory, name); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(session.Dir(directory, name), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := session.Restore(directory, name); !errors.Is(err, session.ErrAlreadyStored) {
		t.Errorf("expected a restore over a stored session to be refused, got %v", err)
	}
}

func TestAnArchiveCannotWriteOutsideTheSessionDirectory(t *testing.T) {
	directory := t.TempDir()
	name := "wiry-turtle"

	var packed bytes.Buffer
	compressor := gzip.NewWriter(&packed)
	archive := tar.NewWriter(compressor)
	header := &tar.Header{Name: "../escaped.txt", Mode: 0o600, Size: 1, Typeflag: tar.TypeReg}
	if err := archive.WriteHeader(header); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressor.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(session.ArchivePath(directory, name), packed.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := session.Restore(directory, name); err == nil {
		t.Error("expected the escaping entry to be refused")
	}
	if _, err := os.Stat(filepath.Join(directory, "escaped.txt")); !os.IsNotExist(err) {
		t.Errorf("expected nothing to have been written outside the session, got %v", err)
	}
}
