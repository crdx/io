package agent

import (
	"context"
	"errors"
	"time"
)

const (
	RetryAttempts = 3

	retryFirstWait = 250 * time.Millisecond
	retryMaxWait   = 4 * time.Second

	retryPatience = time.Minute
)

func retryWait(err error, attempt int) (time.Duration, bool) {
	var retriable Retriable

	if attempt >= RetryAttempts || !errors.As(err, &retriable) || !retriable.Retriable() {
		return 0, false
	}

	switch asked := retriable.RetryAfter(); {
	case asked > retryPatience:
		return 0, false
	case asked > 0:
		return asked, true
	}

	return backoff(attempt), true
}

func backoff(attempt int) time.Duration {
	return min(retryFirstWait<<(attempt-1), retryMaxWait)
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
