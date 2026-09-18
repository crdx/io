package stop

import (
	"context"
	"errors"
)

type stopError struct {
	sentence string
	cause    error
}

func (self stopError) Error() string { return self.sentence }
func (self stopError) Unwrap() error { return self.cause }

func Because(sentence string) error {
	return stopError{sentence: sentence, cause: context.Canceled}
}

func Sentence(ctx context.Context) string {
	if why, ok := errors.AsType[stopError](context.Cause(ctx)); ok {
		return why.sentence
	}

	return ""
}

func Phrase(ctx context.Context) string {
	if sentence := Sentence(ctx); sentence != "" {
		return " because " + sentence
	}

	return ""
}

func Error(ctx context.Context, subject string) error {
	prefix := ""
	if subject != "" {
		prefix = subject + " "
	}

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return stopError{sentence: prefix + "ran out of time", cause: context.DeadlineExceeded}
	}

	return stopError{sentence: prefix + "stopped" + Phrase(ctx), cause: context.Canceled}
}
