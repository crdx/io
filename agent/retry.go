package agent

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"time"
)

const (
	RetryAttempts  = 25
	retryFirstWait = 250 * time.Millisecond
	retryMaxWait   = time.Minute
	retryBudget    = 15 * time.Minute
)

func (self *Agent) retryWait(err error, attempt int, spent time.Duration) (time.Duration, bool) {
	var retriable Retriable

	if attempt >= RetryAttempts || !errors.As(err, &retriable) || !retriable.Retriable() {
		return 0, false
	}

	wait := self.jittered(backoff(attempt))
	if asked := retriable.RetryAfter(); asked > 0 {
		wait = asked
	}

	if spent+wait > retryBudget {
		return 0, false
	}

	return wait, true
}

func backoff(attempt int) time.Duration {
	return min(retryFirstWait<<(attempt-1), retryMaxWait)
}

func (self *Agent) jittered(wait time.Duration) time.Duration {
	if self.retryWaitsPassAtOnce {
		return wait
	}

	//nolint:gosec // a wait must be spread, not unguessable
	return wait/2 + time.Duration(rand.Int64N(int64(wait/2)+1))
}

func isResumable(err error) bool {
	var resumable Resumable

	return errors.As(err, &resumable) && resumable.Resumable()
}

func faultedCall(err error) (ToolCall, bool) {
	var faulted CallFaulted

	if !errors.As(err, &faulted) {
		return ToolCall{}, false
	}

	return faulted.FaultedCall(), true
}

type rewind struct {
	state State
	items []json.RawMessage
}

func rewindOf(provider Provider) *rewind {
	state, isRewindable := provider.(State)
	if !isRewindable {
		return nil
	}

	return &rewind{state: state, items: cloneState(state.Dump())}
}

func (self *rewind) restore() {
	if self == nil {
		return
	}

	self.state.Load(cloneState(self.items))
}

func (self *Agent) TakeRetryWaitsAtOnce() {
	self.retryWaitsPassAtOnce = true
}

func (self *Agent) waitBeforeRetry(ctx context.Context, wait time.Duration) bool {
	if self.retryWaitsPassAtOnce {
		return ctx.Err() == nil
	}

	return waitFor(ctx, wait)
}

func waitFor(ctx context.Context, wait time.Duration) bool {
	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
