package agent_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/tool"
)

type wireDiedError struct {
	wait time.Duration
}

func (wireDiedError) Error() string                  { return "the stream ended before the response did" }
func (wireDiedError) Retriable() bool                { return true }
func (self wireDiedError) RetryAfter() time.Duration { return self.wait }

type flatRefusalError struct{}

func (flatRefusalError) Error() string             { return "that request was no good" }
func (flatRefusalError) Retriable() bool           { return false }
func (flatRefusalError) RetryAfter() time.Duration { return 0 }

type failingProvider struct {
	failures int   // how many of the first requests fail
	err      error // what they fail with

	sent int // how many requests were made in all
}

func (self *failingProvider) Configure(string, []tool.Definition)   {}
func (self *failingProvider) AddUserMessage(string)                 {}
func (self *failingProvider) AddToolResults([]agent.ToolCallResult) {}

func (self *failingProvider) Send(_ context.Context, yield agent.Yield) (agent.Reply, error) {
	self.sent++

	if self.sent <= self.failures {
		yield(agent.Output{Kind: agent.ModelMessageEvent, Text: "half an ans"})

		return agent.Reply{}, self.err
	}

	yield(agent.Output{Kind: agent.ModelMessageEvent, Text: "wer"})
	yield(agent.Output{Kind: agent.ModelMessageEvent, Done: true})

	return agent.Reply{}, nil
}

func collect(t *testing.T, assistant *agent.Agent) ([]agent.Event, error) {
	t.Helper()

	var seen []agent.Event
	var failure error

	synctest.Test(t, func(t *testing.T) {
		for update, err := range assistant.Stream(t.Context(), "go") {
			if err != nil {
				failure = err

				break
			}

			if update.Event != nil {
				seen = append(seen, *update.Event)
			}
		}
	})

	return seen, failure
}

func kinds(events []agent.Event) []agent.Kind {
	seen := make([]agent.Kind, len(events))
	for i, event := range events {
		seen[i] = event.Kind
	}

	return seen
}

func TestAFailedRequestThatIsWorthRepeatingIsMadeAgain(t *testing.T) {
	provider := &failingProvider{failures: 1, err: wireDiedError{}}
	assistant := agent.New("", provider, nil)

	events, err := collect(t, assistant)
	if err != nil {
		t.Fatalf("expected the turn to recover, got %v", err)
	}

	if provider.sent != 2 {
		t.Errorf("expected the request to be made twice, got %d", provider.sent)
	}

	var said strings.Builder

	for _, event := range events {
		if event.Kind == agent.ModelMessageEvent {
			said.WriteString(event.Text)
		}
	}

	if said.String() != "half an answer" {
		t.Errorf("expected the answer to carry on where it stopped, got %q", said.String())
	}
}

func TestAFailedRequestSaysSoBeforeItIsMadeAgain(t *testing.T) {
	provider := &failingProvider{failures: 1, err: wireDiedError{}}
	assistant := agent.New("", provider, nil)

	events, err := collect(t, assistant)
	if err != nil {
		t.Fatal(err)
	}

	var notices []agent.Event
	for _, event := range events {
		if event.Kind == agent.RetryingEvent {
			notices = append(notices, event)
		}
	}

	if len(notices) != 1 {
		t.Fatalf("expected one retry to be reported, got %v", kinds(events))
	}

	switch {
	case notices[0].Attempt != 1:
		t.Errorf("expected the failed attempt to be numbered, got %d", notices[0].Attempt)
	case notices[0].Took <= 0:
		t.Errorf("expected the wait to be reported, got %s", notices[0].Took)
	case notices[0].Text != wireDiedError{}.Error():
		t.Errorf("expected what stopped it to be reported, got %q", notices[0].Text)
	}
}

func TestARequestThatKeepsFailingGivesUpAndReportsTheLastFailure(t *testing.T) {
	provider := &failingProvider{failures: 100, err: wireDiedError{}}
	assistant := agent.New("", provider, nil)

	_, err := collect(t, assistant)

	if _, ok := errors.AsType[wireDiedError](err); !ok {
		t.Fatalf("expected the failure to be reported, got %v", err)
	}

	if provider.sent != agent.RetryAttempts {
		t.Errorf("expected %d attempts in all, got %d", agent.RetryAttempts, provider.sent)
	}
}

func TestAFailureNotWorthRepeatingIsReportedStraightAway(t *testing.T) {
	tests := map[string]error{
		"the endpoint refused the request":  flatRefusalError{},
		"nothing said whether to try again": errors.New("something went wrong"),
	}

	for name, failure := range tests {
		t.Run(name, func(t *testing.T) {
			provider := &failingProvider{failures: 100, err: failure}
			assistant := agent.New("", provider, nil)

			if _, err := collect(t, assistant); err == nil {
				t.Fatal("expected the failure to be reported")
			}

			if provider.sent != 1 {
				t.Errorf("expected one attempt and no more, got %d", provider.sent)
			}
		})
	}
}

func TestAnEndpointAskingToBeLeftAloneForTooLongIsNotWaitedFor(t *testing.T) {
	provider := &failingProvider{failures: 100, err: wireDiedError{wait: time.Hour}}
	assistant := agent.New("", provider, nil)

	if _, err := collect(t, assistant); err == nil {
		t.Fatal("expected the failure to be reported")
	}

	if provider.sent != 1 {
		t.Errorf("expected one attempt and no more, got %d", provider.sent)
	}
}

func TestAnEndpointAskingToBeLeftAloneIsWaitedForAsLongAsItAsked(t *testing.T) {
	const asked = 40 * time.Millisecond

	provider := &failingProvider{failures: 1, err: wireDiedError{wait: asked}}
	assistant := agent.New("", provider, nil)

	events, err := collect(t, assistant)
	if err != nil {
		t.Fatal(err)
	}

	for _, event := range events {
		if event.Kind == agent.RetryingEvent && event.Took != asked {
			t.Errorf("expected the wait the endpoint asked for, got %s", event.Took)
		}
	}
}

func TestStoppingTheTurnDuringAWaitEndsItThereAndThen(t *testing.T) {
	provider := &failingProvider{failures: 100, err: wireDiedError{}}
	assistant := agent.New("", provider, nil)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	started := time.Now()

	for update, err := range assistant.Stream(ctx, "go") {
		if err != nil {
			break
		}

		if update.Event != nil && update.Event.Kind == agent.RetryingEvent {
			cancel()
		}
	}

	if provider.sent != 1 {
		t.Errorf("expected the wait to be given up on, got %d attempts", provider.sent)
	}

	if waited := time.Since(started); waited > time.Second {
		t.Errorf("expected the wait to end when the turn did, and it took %s", waited)
	}
}

func TestNothingIsRepeatedOnceTheCallerHasStoppedListening(t *testing.T) {
	provider := &failingProvider{failures: 100, err: wireDiedError{}}
	assistant := agent.New("", provider, []tool.Tool{noop()})

	for update := range assistant.Stream(t.Context(), "go") {
		if update.Delta != nil && update.Delta.Kind == agent.ModelMessageEvent {
			break
		}
	}

	if provider.sent != 1 {
		t.Errorf("expected an abandoned turn not to be retried, got %d attempts", provider.sent)
	}
}
