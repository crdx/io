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

func TestCarriageReturnsAreNotPartOfThePayload(t *testing.T) {
	seen, err := collect("data: one\r\n\r\n", "one")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !slices.Equal(seen, []string{"one"}) {
		t.Errorf("expected the payload without the carriage return, got %q", seen)
	}
}

func TestALoneCarriageReturnStaysInThePayload(t *testing.T) {
	seen, err := collect("data: one\rtwo\n\n", "one\rtwo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !slices.Equal(seen, []string{"one\rtwo"}) {
		t.Errorf("expected the payload whole, got %q", seen)
	}
}

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

func TestAStreamThatEndsEarlyIsTruncated(t *testing.T) {
	seen, err := collect("data: one\n\n", "end")
	if !errors.Is(err, sse.ErrTruncated) {
		t.Fatalf("expected a truncated stream to be reported, got %v", err)
	}

	if !slices.Equal(seen, []string{"one"}) {
		t.Errorf("expected what did arrive, got %v", seen)
	}
}

func TestReadingStopsWhenTheReaderIsDone(t *testing.T) {
	seen, err := collect("data: end\n\ndata: two\n\n", "end")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !slices.Equal(seen, []string{"end"}) {
		t.Errorf("expected nothing after the end, got %v", seen)
	}
}

func fields(stream string) string {
	var out strings.Builder

	for line := range strings.SplitSeq(stream, "\n") {
		field, carried := strings.CutPrefix(strings.TrimRight(line, "\r"), "data:")
		if carried {
			out.WriteString(strings.TrimSpace(field))
		}
	}

	return out.String()
}

func FuzzRead(f *testing.F) {
	for _, seed := range []string{
		"data: one\n\n",
		"data: one\ndata: two\n\ndata: end",
		"\n\n\n",
		"data:\n\n",
		"data:   spaced   \r\n\r\n",
		": comment\nevent: message\nid: 1\nretry: 500\n\n",
		"data",
		"datadata: one\n\n",
		"\r\n",
		"data: \xff\x00\n\n",
		"data:0\r0",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, stream string) {
		var seen []string

		err := sse.Read(strings.NewReader(stream), func(payload string) (bool, error) {
			seen = append(seen, payload)
			return false, nil
		})

		if !errors.Is(err, sse.ErrTruncated) {
			t.Fatalf("expected the stream to run out, got %v", err)
		}

		for _, payload := range seen {
			if payload == "" {
				t.Error("expected no empty payload")
			}

			if strings.Contains(payload, "\n") {
				t.Errorf("expected no newline in a payload, got %q", payload)
			}
		}

		if joined := strings.Join(seen, ""); joined != fields(stream) {
			t.Errorf("expected the data fields, got %q", joined)
		}
	})
}

func TestTheReadersOwnFailureIsHandedBack(t *testing.T) {
	refused := errors.New("that made no sense")

	err := sse.Read(strings.NewReader("data: one\n\ndata: two\n\n"),
		func(string) (bool, error) { return false, refused })

	if !errors.Is(err, refused) {
		t.Errorf("expected the reader's own failure, got %v", err)
	}
}
