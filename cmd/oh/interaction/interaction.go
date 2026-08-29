package interaction

import (
	"bufio"
	"os"
	"os/signal"
	"syscall"
	"time"

	"crdx.org/io/cmd/oh/key"
	"crdx.org/io/cmd/oh/tty"
	"crdx.org/io/cmd/oh/turn"
)

const (
	settling  = 100 * time.Millisecond
	heartRate = 15 * time.Second
	soonest   = time.Millisecond
)

type Handler struct {
	Events       func() <-chan turn.Event
	Key          func(key.Key) bool
	Turn         func(turn.Event)
	TurnFinished func() bool
	Resize       func()
	Beat         func()
	Changes      <-chan error
	Change       func(error) bool
	Draw         func()
}

func Run(terminal *os.File, getNextRefresh func(time.Time) time.Time, handler Handler) {
	resizeSignals := Resizes()
	defer signal.Stop(resizeSignals)

	refresh := newRefreshTimer(getNextRefresh)
	defer refresh.stop()

	beater := time.NewTicker(heartRate)
	defer beater.Stop()

	keys, stopReading := Keypresses(terminal)
	defer stopReading()

	run(keys, resizeSignals, refresh.timer.C, refresh.schedule, beater.C, handler)
}

func run(keys <-chan key.Key, resizeSignals <-chan os.Signal, refreshes <-chan time.Time, schedule func(), beats <-chan time.Time, handler Handler) {
	changes := handler.Changes
	for {
		schedule()

		select {
		case keypress, open := <-keys:
			if !open || !handler.Key(keypress) {
				return
			}
		case event, running := <-handler.Events():
			if running {
				handler.Turn(event)
			} else if !handler.TurnFinished() {
				return
			}
		case <-resizeSignals:
			Settle(resizeSignals)
			handler.Resize()
		case <-beats:
			handler.Beat()

			select {
			case <-refreshes:
			default:
				continue
			}
		case <-refreshes:
		case failure, open := <-changes:
			if !open {
				changes = nil
				continue
			}
			if handler.Change != nil && !handler.Change(failure) {
				continue
			}
		}

		handler.Draw()
	}
}

type refreshTimer struct {
	getNextRefresh func(time.Time) time.Time
	timer          *time.Timer
}

func newRefreshTimer(getNextRefresh func(time.Time) time.Time) *refreshTimer {
	timer := time.NewTimer(time.Hour)
	timer.Stop()

	return &refreshTimer{getNextRefresh: getNextRefresh, timer: timer}
}

func (self *refreshTimer) schedule() {
	at := time.Now()

	dueAt := self.getNextRefresh(at)
	if dueAt.IsZero() {
		self.stop()
		return
	}

	if delay := dueAt.Sub(at); delay > 0 {
		self.timer.Reset(delay)
	} else {
		self.timer.Reset(soonest)
	}
}

func (self *refreshTimer) stop() {
	self.timer.Stop()
}

// Keypresses decodes keys until the returned stop is called, which waits for the reading to finish
// so that the terminal is nobody's before it returns.
func Keypresses(terminal *os.File) (<-chan key.Key, func()) {
	reader := tty.NewReader(terminal)
	keys := make(chan key.Key)
	finished := make(chan struct{})

	go func() {
		defer close(finished)
		defer close(keys)

		decoder := key.NewDecoder(bufio.NewReader(reader))
		for {
			keypress, err := decoder.Next()
			if err != nil {
				return
			}

			select {
			case keys <- keypress:
			case <-reader.Stopping():
				return
			}
		}
	}()

	return keys, func() {
		reader.Stop()
		<-finished
		reader.Close()
	}
}

func Resizes() chan os.Signal {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGWINCH)
	return signals
}

func Settle(signals <-chan os.Signal) {
	time.Sleep(settling)
	for {
		select {
		case <-signals:
		default:
			return
		}
	}
}
