package jobs

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"crdx.org/io/internal/sandbox"
	"crdx.org/io/internal/util"
)

const gracePeriod = 5 * time.Second

type State string

const (
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopping State = "stopping"
	StateComplete State = "complete"
	StateFailed   State = "failed"
	StateStopped  State = "stopped"
	StateEnded    State = "ended with the session"
)

var (
	ErrClosed   = errors.New("no more jobs can be started in this session")
	ErrTaken    = errors.New("a job of that name is already running")
	ErrNotFound = errors.New("there is no job of that name")
)

type Snapshot struct {
	Name         string    `json:"name"`
	Command      string    `json:"command"`
	State        State     `json:"state"`
	StartedAt    time.Time `json:"started_at"`
	EndedAt      time.Time `json:"ended_at,omitzero"`
	Code         int       `json:"code,omitempty"`
	Failure      string    `json:"failure,omitempty"`
	DroppedBytes int       `json:"-"`
}

func (self Snapshot) IsLive() bool { return isLive(self.State) }

func (self Snapshot) Describe() string {
	parts := []string{self.Name + ": " + string(self.State)}

	if self.IsLive() {
		parts = append(parts, "up "+util.CompactDuration(time.Since(self.StartedAt).Round(time.Second)))
	} else if !self.EndedAt.IsZero() {
		parts = append(parts, "ran for "+util.CompactDuration(self.EndedAt.Sub(self.StartedAt).Round(time.Second)))
	}

	if self.Code != 0 {
		parts = append(parts, fmt.Sprintf("exit(%d)", self.Code))
	}

	if self.Failure != "" {
		parts = append(parts, self.Failure)
	}

	return strings.Join(parts, ", ")
}

type job struct {
	name      string
	command   string
	state     State
	startedAt time.Time
	endedAt   time.Time
	code      int
	failure   string
	policy    sandbox.Policy
	output    *spool
	running   sandbox.Command
	over      chan struct{}
}

type Manager struct {
	runner   sandbox.Runner
	mutex    sync.Mutex
	jobs     map[string]*job
	order    []string
	isClosed bool
	watching sync.WaitGroup
}

func New(runner sandbox.Runner) *Manager {
	return &Manager{runner: runner, jobs: make(map[string]*job)}
}

func (self *Manager) Restore(rememberedJobs []Snapshot) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	for _, snapshot := range rememberedJobs {
		if snapshot.Name == "" || self.jobs[snapshot.Name] != nil {
			continue
		}

		state := snapshot.State
		if isLive(state) {
			state = StateEnded
		}

		self.jobs[snapshot.Name] = &job{
			name:      snapshot.Name,
			command:   snapshot.Command,
			state:     state,
			startedAt: snapshot.StartedAt,
			endedAt:   snapshot.EndedAt,
			code:      snapshot.Code,
			failure:   snapshot.Failure,
			output:    &spool{},
			over:      closedChannel(),
		}
		self.order = append(self.order, snapshot.Name)
	}
}

func closedChannel() chan struct{} {
	over := make(chan struct{})
	close(over)

	return over
}

func (self *Manager) RememberedCommand(name string) (string, bool) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	found, isKnown := self.jobs[name]
	if !isKnown || found.command == "" {
		return "", false
	}

	return found.command, true
}

func (self *Manager) Start(
	ctx context.Context,
	name string,
	directory string,
	command string,
	policy sandbox.Policy,
) (Snapshot, error) {
	opening, err := self.claim(name, command, policy)
	if err != nil {
		return Snapshot{}, err
	}

	running, err := self.runner.Start(context.WithoutCancel(ctx), directory, command, policy, opening.output)
	if err != nil {
		self.conclude(opening, StateFailed, 0, err.Error())
		return self.snapshot(opening), fmt.Errorf("the job could not be started: %w", err)
	}

	if !self.settleStarted(opening, running) {
		running.Stop()
	}

	self.watching.Add(1)

	go self.watch(opening)

	return self.snapshot(opening), nil
}

func (self *Manager) Stop(name string) (Snapshot, error) {
	self.mutex.Lock()
	found, isKnown := self.jobs[name]
	self.mutex.Unlock()

	if !isKnown {
		return Snapshot{}, ErrNotFound
	}

	self.end(found)

	return self.snapshot(found), nil
}

func (self *Manager) Status(name string) (Snapshot, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	found, isKnown := self.jobs[name]
	if !isKnown {
		return Snapshot{}, ErrNotFound
	}

	return self.describe(found), nil
}

func (self *Manager) Output(name string) (string, Snapshot, error) {
	self.mutex.Lock()
	found, isKnown := self.jobs[name]
	self.mutex.Unlock()

	if !isKnown {
		return "", Snapshot{}, ErrNotFound
	}

	return found.output.String(), self.snapshot(found), nil
}

func (self *Manager) List() []Snapshot {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	listing := make([]Snapshot, 0, len(self.order))
	for _, name := range self.order {
		listing = append(listing, self.describe(self.jobs[name]))
	}

	return listing
}

func (self *Manager) Discard(name string) (Snapshot, error) {
	self.mutex.Lock()
	found, isKnown := self.jobs[name]
	self.mutex.Unlock()

	if !isKnown {
		return Snapshot{}, ErrNotFound
	}

	self.end(found)

	discardedJob := self.snapshot(found)

	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.remove(name)

	return discardedJob, nil
}

func (self *Manager) PruneFinished() []string {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	var prunedNames []string

	for _, name := range slices.Clone(self.order) {
		if isLive(self.jobs[name].state) {
			continue
		}

		prunedNames = append(prunedNames, name)
		self.remove(name)
	}

	return prunedNames
}

func (self *Manager) StopHolding(holds func(sandbox.Policy) bool) []string {
	self.mutex.Lock()
	var stopping []*job
	for _, name := range self.order {
		found := self.jobs[name]
		if isLive(found.state) && holds(found.policy) {
			stopping = append(stopping, found)
		}
	}
	self.mutex.Unlock()

	names := make([]string, 0, len(stopping))
	for _, ending := range stopping {
		_ = self.beginEnd(ending)
		self.watching.Go(func() { self.end(ending) })
		names = append(names, ending.name)
	}

	return names
}

func (self *Manager) Close() error {
	self.mutex.Lock()
	self.isClosed = true
	live := make([]*job, 0, len(self.jobs))
	for _, found := range self.jobs {
		if isLive(found.state) {
			live = append(live, found)
		}
	}
	self.mutex.Unlock()

	var ending sync.WaitGroup
	for _, found := range live {
		ending.Go(func() { self.end(found) })
	}
	ending.Wait()

	self.watching.Wait()

	return nil
}

func (self *Manager) settleStarted(opening *job, running sandbox.Command) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	opening.running = running

	if opening.state != StateStarting {
		return false
	}

	opening.state = StateRunning

	return true
}

func (self *Manager) remove(name string) {
	delete(self.jobs, name)
	self.order = slices.DeleteFunc(self.order, func(remaining string) bool { return remaining == name })
}

func (self *Manager) claim(name string, command string, policy sandbox.Policy) (*job, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if self.isClosed {
		return nil, ErrClosed
	}

	if strings.TrimSpace(name) == "" {
		return nil, errors.New("a job needs a name to be addressed by")
	}

	if found, isKnown := self.jobs[name]; isKnown && isLive(found.state) {
		return nil, ErrTaken
	}

	opening := &job{
		name:      name,
		command:   command,
		state:     StateStarting,
		startedAt: time.Now(),
		policy:    policy,
		output:    &spool{},
		over:      make(chan struct{}),
	}

	if !slices.Contains(self.order, name) {
		self.order = append(self.order, name)
	}
	self.jobs[name] = opening

	return opening, nil
}

func (self *Manager) watch(ending *job) {
	defer self.watching.Done()
	defer close(ending.over)

	result, err := ending.running.Wait()

	switch {
	case err != nil:
		self.conclude(ending, self.endingState(ending, StateFailed), result.Code, err.Error())
	case result.Code != 0:
		self.conclude(ending, self.endingState(ending, StateFailed), result.Code, "")
	default:
		self.conclude(ending, self.endingState(ending, StateComplete), 0, "")
	}
}

func (self *Manager) endingState(ending *job, natural State) State {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if ending.state == StateStopping {
		return StateStopped
	}

	return natural
}

func (self *Manager) conclude(ending *job, state State, code int, failure string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	ending.state = state
	ending.code = code
	ending.failure = failure
	ending.endedAt = time.Now()
}

func (self *Manager) beginEnd(ending *job) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if !isLive(ending.state) {
		return false
	}

	ending.state = StateStopping

	return true
}

func (self *Manager) end(ending *job) {
	if !self.beginEnd(ending) {
		return
	}

	self.mutex.Lock()
	running := ending.running
	self.mutex.Unlock()

	if running == nil {
		self.conclude(ending, StateStopped, 0, "")

		return
	}

	_ = running.Signal(syscall.SIGTERM)

	select {
	case <-ending.over:
		return
	case <-time.After(gracePeriod):
	}

	running.Stop()
	<-ending.over
}

func (self *Manager) snapshot(subject *job) Snapshot {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return self.describe(subject)
}

func (self *Manager) describe(subject *job) Snapshot {
	return Snapshot{
		Name:         subject.name,
		Command:      subject.command,
		State:        subject.state,
		StartedAt:    subject.startedAt,
		EndedAt:      subject.endedAt,
		Code:         subject.code,
		Failure:      subject.failure,
		DroppedBytes: subject.output.DroppedBytes(),
	}
}

func isLive(state State) bool {
	return state == StateStarting || state == StateRunning || state == StateStopping
}
