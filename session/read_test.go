package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadRejectsASessionWithoutAHead(t *testing.T) {
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "broken"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "broken", "session.jsonl"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(directory, "broken"); err == nil {
		t.Error("expected an empty session to be rejected")
	}
}

func TestSessionIDsCannotEscapeTheSessionDirectory(t *testing.T) {
	if _, err := Read(t.TempDir(), "../somewhere-else"); err == nil {
		t.Error("expected a path-like session ID to be rejected")
	}
}

func TestEveryBitOfAnIDSurvivesBeingWritten(t *testing.T) {
	var least, most [16]byte
	for i := range most {
		most[i] = 0xff
	}

	tests := map[[16]byte]string{
		least: "0000000000000000000000",
		most:  "7n42DGM5Tflk9n8mt7Fhc7",
	}
	for id, want := range tests {
		if got := encode(id); got != want {
			t.Errorf("encode(%x) = %q, want %q", id, got, want)
		}
	}
}
