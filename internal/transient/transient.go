package transient

import (
	"context"
	"errors"
	"time"
)

type failure struct{ cause error }

func (self failure) Error() string { return self.cause.Error() }

func (self failure) Unwrap() error { return self.cause }

func (failure) Retriable() bool { return true }

func (failure) RetryAfter() time.Duration { return 0 }

func Wrap(err error) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	return failure{cause: err}
}
