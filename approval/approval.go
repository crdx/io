package approval

import (
	"context"
	"errors"
	"sync"
)

var (
	ErrDenied      = errors.New("approval was denied")
	ErrUnavailable = errors.New("interactive approval is unavailable")
)

type Prompt struct {
	Question string
	Detail   string
	Language string
}

type Request struct {
	Prompt     Prompt
	answer     chan bool
	answerOnce sync.Once
	finishOnce sync.Once
	finish     func()
}

func (self *Request) Answer(isApproved bool) {
	self.answerOnce.Do(func() {
		self.answer <- isApproved
		self.finishRequest()
	})
}

func (self *Request) finishRequest() {
	self.finishOnce.Do(self.finish)
}

type Broker struct {
	mutex         sync.Mutex
	requests      []*Request
	changes       chan struct{}
	isInteractive bool
}

func New() *Broker {
	return &Broker{changes: make(chan struct{}, 1)}
}

func (self *Broker) Open() func() {
	self.mutex.Lock()
	self.isInteractive = true
	self.mutex.Unlock()

	return func() {
		self.mutex.Lock()
		self.isInteractive = false
		self.mutex.Unlock()
	}
}

func (self *Broker) Changes() <-chan struct{} {
	return self.changes
}

func (self *Broker) Current() *Request {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if len(self.requests) == 0 {
		return nil
	}

	return self.requests[0]
}

func (self *Broker) Ask(ctx context.Context, prompt Prompt) error {
	request := &Request{Prompt: prompt, answer: make(chan bool, 1)}
	request.finish = func() { self.finish(request) }

	self.mutex.Lock()
	if !self.isInteractive {
		self.mutex.Unlock()
		return ErrUnavailable
	}
	self.requests = append(self.requests, request)
	self.mutex.Unlock()
	self.changed()

	defer request.finishRequest()

	select {
	case isApproved := <-request.answer:
		if !isApproved {
			return ErrDenied
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (self *Broker) finish(request *Request) {
	self.mutex.Lock()
	for i, current := range self.requests {
		if current == request {
			self.requests = append(self.requests[:i], self.requests[i+1:]...)
			break
		}
	}
	self.mutex.Unlock()
	self.changed()
}

func (self *Broker) changed() {
	select {
	case self.changes <- struct{}{}:
	default:
	}
}
