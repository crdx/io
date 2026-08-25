package web

import (
	"context"
	"errors"
	"testing"
)

type searchStub struct {
	query  string
	output string
	err    error
}

func (self *searchStub) Search(_ context.Context, query string) (string, error) {
	self.query = query
	return self.output, self.err
}

func TestSearchDelegatesToTheConfiguredSearcher(t *testing.T) {
	searcher := &searchStub{output: "cited answer"}
	call, err := newSearch(func() bool { return true }, searcher).Parse(`{"query":"current weather"}`)
	if err != nil {
		t.Fatal(err)
	}

	result, err := call.Exec(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if searcher.query != "current weather" || result.Output != "cited answer" {
		t.Errorf("got query %q and output %q", searcher.query, result.Output)
	}
}

func TestSearchReportsTheSearchersFailure(t *testing.T) {
	failure := errors.New("search failed")
	call, err := newSearch(func() bool { return true }, &searchStub{err: failure}).Parse(`{"query":"weather"}`)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := call.Exec(t.Context()); !errors.Is(err, failure) {
		t.Errorf("got %v", err)
	}
}

func TestSearchRejectsAnEmptyQuery(t *testing.T) {
	if _, err := newSearch(func() bool { return true }, &searchStub{}).Parse(`{"query":"  "}`); err == nil {
		t.Error("expected an empty query to be rejected")
	}
}
