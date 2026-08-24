package strutil_test

import (
	"reflect"
	"testing"

	"crdx.org/io/internal/util/strutil"
)

func TestLinesDoesNotAddALineAfterATrailingNewline(t *testing.T) {
	got := strutil.Lines("one\ntwo\n")
	want := []string{"one", "two"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestLinesKeepsEmptyLines(t *testing.T) {
	got := strutil.Lines("one\n\ntwo")
	want := []string{"one", "", "two"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestNoTextHasNoLines(t *testing.T) {
	if got := strutil.Lines(""); got != nil {
		t.Errorf("got %#v, want nil", got)
	}
}
