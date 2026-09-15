package agent_test

import (
	"errors"
	"fmt"
	"testing"

	"crdx.org/io/agent"
)

type describedFailureError struct{}

func (describedFailureError) Error() string { return "rendered prose that should not be stored" }

func (describedFailureError) DescribeFailure() agent.Failure {
	return agent.Failure{
		Kind:       agent.HTTPStatusFailure,
		HTTPStatus: 598,
		Code:       "future_edge_error",
		MediaType:  "text/html",
	}
}

func TestFailureFromKeepsStructuredDetailsThroughAWrappedError(t *testing.T) {
	failure := agent.FailureFrom(fmt.Errorf("provider failed: %w", describedFailureError{}))
	if failure == nil {
		t.Fatal("expected a failure")
	}

	want := agent.Failure{
		Kind:       agent.HTTPStatusFailure,
		Context:    "provider failed",
		HTTPStatus: 598,
		Code:       "future_edge_error",
		MediaType:  "text/html",
	}
	if *failure != want {
		t.Errorf("got %+v, want %+v", *failure, want)
	}
}

func TestFailureFromFallsBackToAnUnhandledErrorsMessage(t *testing.T) {
	failure := agent.FailureFrom(errors.New("something entirely new happened"))
	if failure == nil {
		t.Fatal("expected a failure")
	}
	if failure.Kind != agent.GenericFailure || failure.Message != "something entirely new happened" {
		t.Errorf("got %+v", *failure)
	}
}

func TestFailureTextRendersSemanticFieldsAndOldText(t *testing.T) {
	tests := map[string]struct {
		event agent.Event
		want  string
	}{
		"endpoint message": {
			event: agent.Event{Failure: &agent.Failure{
				Kind:       agent.HTTPStatusFailure,
				Message:    "slow down",
				HTTPStatus: 429,
			}},
			want: "slow down",
		},
		"http body": {
			event: agent.Event{Failure: &agent.Failure{
				Kind:       agent.HTTPStatusFailure,
				HTTPStatus: 598,
				Body:       "future details",
			}},
			want: "request failed with status 598: future details",
		},
		"http status": {
			event: agent.Event{Failure: &agent.Failure{
				Kind:       agent.HTTPStatusFailure,
				HTTPStatus: 520,
				MediaType:  "text/html",
			}},
			want: "request failed with status 520",
		},
		"http context": {
			event: agent.Event{Failure: &agent.Failure{
				Kind:       agent.HTTPStatusFailure,
				Context:    "refresh credentials",
				HTTPStatus: 500,
				Body:       `{"error":"server_error"}`,
			}},
			want: `refresh credentials: request failed with status 500: {"error":"server_error"}`,
		},
		"generic error": {
			event: agent.Event{Failure: &agent.Failure{
				Kind:    agent.GenericFailure,
				Message: "the stream ended",
			}},
			want: "the stream ended",
		},
		"old text": {
			event: agent.Event{Text: "an error recorded before structured failures"},
			want:  "an error recorded before structured failures",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := agent.FailureText(test.event); got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}
