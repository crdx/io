package sse_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"crdx.org/io/internal/sse"
)

func collect(stream string, last string) ([]string, error) {
	var seen []string

	err := sse.Read(strings.NewReader(stream), func(payload string) (bool, error) {
		seen = append(seen, payload)
		return payload == last, nil
	})

	return seen, err
}

// A frame is whatever arrived before the blank line, and the lines it was spread over are one
// payload by the time anyone sees it.
func TestFramesArriveOneAtATime(t *testing.T) {
	seen, err := collect("data: one\n\ndata: two\ndata: three\n\ndata: end\n\n", "end")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"one", "twothree", "end"}
	if !slices.Equal(seen, expected) {
		t.Errorf("expected %v, got %v", expected, seen)
	}
}

// A stream carries more than data, and the rest of it is somebody else's business.
func TestOtherFieldsAreIgnored(t *testing.T) {
	stream := ": keeping the line warm\nevent: message\nid: 1\ndata: one\nretry: 500\n\n"

	seen, err := collect(stream, "one")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !slices.Equal(seen, []string{"one"}) {
		t.Errorf("expected the data alone, got %v", seen)
	}
}

// A blank line with nothing in front of it closes nothing, so nobody hears about it.
func TestEmptyFramesAreNotDispatched(t *testing.T) {
	seen, err := collect("\n\ndata: one\n\n\n\ndata: end\n\n", "end")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"one", "end"}
	if !slices.Equal(seen, expected) {
		t.Errorf("expected %v, got %v", expected, seen)
	}
}

// The wire says CRLF, and a payload should not arrive wearing it.
func TestCarriageReturnsAreNotPartOfThePayload(t *testing.T) {
	seen, err := collect("data: one\r\n\r\n", "one")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !slices.Equal(seen, []string{"one"}) {
		t.Errorf("expected the payload without the carriage return, got %q", seen)
	}
}

// A carriage return that ends no line is nothing to do with the framing, and belongs to whoever
// asked for the payload.
func TestALoneCarriageReturnStaysInThePayload(t *testing.T) {
	seen, err := collect("data: one\rtwo\n\n", "one\rtwo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !slices.Equal(seen, []string{"one\rtwo"}) {
		t.Errorf("expected the payload whole, got %q", seen)
	}
}

// A last frame that the sender never closed off is still a frame, and is worth hearing before the
// stream is called truncated.
func TestTheLastFrameCountsWithoutItsBlankLine(t *testing.T) {
	seen, err := collect("data: one\n\ndata: end", "end")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"one", "end"}
	if !slices.Equal(seen, expected) {
		t.Errorf("expected %v, got %v", expected, seen)
	}
}

// A stream that runs out while the reader is still waiting for the end of it has not been answered,
// however much of it arrived.
func TestAStreamThatEndsEarlyIsTruncated(t *testing.T) {
	seen, err := collect("data: one\n\n", "end")
	if !errors.Is(err, sse.ErrTruncated) {
		t.Fatalf("expected a truncated stream to be reported, got %v", err)
	}

	if !slices.Equal(seen, []string{"one"}) {
		t.Errorf("expected what did arrive, got %v", seen)
	}
}

// Once the reader has heard enough, the rest of the stream is not its concern.
func TestReadingStopsWhenTheReaderIsDone(t *testing.T) {
	seen, err := collect("data: end\n\ndata: two\n\n", "end")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !slices.Equal(seen, []string{"end"}) {
		t.Errorf("expected nothing after the end, got %v", seen)
	}
}

// What the reader makes of a payload is its own affair, and its complaint is the caller's answer.
func TestTheReadersOwnFailureIsHandedBack(t *testing.T) {
	refused := errors.New("that made no sense")

	err := sse.Read(strings.NewReader("data: one\n\ndata: two\n\n"),
		func(string) (bool, error) { return false, refused })

	if !errors.Is(err, refused) {
		t.Errorf("expected the reader's own failure, got %v", err)
	}
}
