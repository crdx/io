package waiting

import (
	"context"
	"sync"
	"time"
)

type key struct{}

type personWait struct {
	mutex sync.Mutex
	total time.Duration
}

func (self *personWait) add(waitedTime time.Duration) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.total += waitedTime
}

func (self *personWait) get() time.Duration {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return self.total
}

func Track(ctx context.Context) (context.Context, func() time.Duration) {
	wait := &personWait{}

	return context.WithValue(ctx, key{}, wait), wait.get
}

func Record(ctx context.Context, waitedTime time.Duration) {
	wait, isTracked := ctx.Value(key{}).(*personWait)
	if !isTracked || waitedTime <= 0 {
		return
	}

	wait.add(waitedTime)
}
