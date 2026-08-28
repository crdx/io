package tty

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestReadLineReadsOnlyOneLine(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = reader.Close()
		_ = writer.Close()
	})

	if _, err := writer.WriteString("redirect\r\nnext\n"); err != nil {
		t.Fatal(err)
	}
	line, err := ReadLine(t.Context(), reader)
	if err != nil {
		t.Fatal(err)
	}
	if line != "redirect" {
		t.Errorf("got %q", line)
	}
}

func TestReadLineStopsWhenCancelled(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = reader.Close()
		_ = writer.Close()
	})

	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		_, err := ReadLine(ctx, reader)
		result <- err
	}()
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled line read remained blocked")
	}
}
