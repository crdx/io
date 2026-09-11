package approval_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"crdx.org/io/approval"
)

func TestApprovalIsUnavailableOutsideInteractiveMode(t *testing.T) {
	broker := approval.New()

	if err := broker.Ask(t.Context(), approval.Prompt{}); !errors.Is(err, approval.ErrUnavailable) {
		t.Errorf("got %v, want interactive approval to be required", err)
	}
}

func TestAResponseAnswersOneRequest(t *testing.T) {
	for name, test := range map[string]struct {
		isApproved bool
		wantErr    error
	}{
		"approved": {isApproved: true},
		"denied":   {wantErr: approval.ErrDenied},
	} {
		t.Run(name, func(t *testing.T) {
			broker := approval.New()
			closeBroker := broker.Open()
			defer closeBroker()

			prompt := approval.Prompt{
				Question: "Run this command?",
				Detail:   "curl example.com",
				Language: "bash",
			}
			result := make(chan error, 1)
			go func() { result <- broker.Ask(t.Context(), prompt) }()

			<-broker.Changes()
			request := broker.Current()
			if request == nil {
				t.Fatal("the approval request was immediately cleared")
			}
			if !reflect.DeepEqual(request.Prompt, prompt) {
				t.Errorf("got prompt %+v, want %+v", request.Prompt, prompt)
			}
			request.Answer(test.isApproved)

			if err := <-result; !errors.Is(err, test.wantErr) {
				t.Errorf("got %v, want %v", err, test.wantErr)
			}
			if request := broker.Current(); request != nil {
				t.Errorf("the completed approval remained current: %+v", request.Prompt)
			}
		})
	}
}

func TestConcurrentRequestsReachTheApproverInOrder(t *testing.T) {
	broker := approval.New()
	closeBroker := broker.Open()
	defer closeBroker()

	firstResult := make(chan error, 1)
	go func() {
		firstResult <- broker.Ask(t.Context(), approval.Prompt{Question: "First?"})
	}()
	<-broker.Changes()

	secondResult := make(chan error, 1)
	go func() {
		secondResult <- broker.Ask(t.Context(), approval.Prompt{Question: "Second?"})
	}()
	<-broker.Changes()

	first := broker.Current()
	if first == nil || first.Prompt.Question != "First?" {
		t.Fatalf("got first request %+v", first)
	}
	first.Answer(true)
	if err := <-firstResult; err != nil {
		t.Fatalf("the first request failed: %v", err)
	}

	second := broker.Current()
	if second == nil || second.Prompt.Question != "Second?" {
		t.Fatalf("got second request %+v", second)
	}
	second.Answer(false)
	if err := <-secondResult; !errors.Is(err, approval.ErrDenied) {
		t.Errorf("got %v, want the second request to be denied", err)
	}
}

func TestCancellationWithdrawsARequest(t *testing.T) {
	broker := approval.New()
	closeBroker := broker.Open()
	defer closeBroker()

	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() { result <- broker.Ask(ctx, approval.Prompt{Question: "Continue?"}) }()
	<-broker.Changes()

	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want cancellation", err)
	}
	if request := broker.Current(); request != nil {
		t.Errorf("the cancelled approval remained current: %+v", request.Prompt)
	}
}
