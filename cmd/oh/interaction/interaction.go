package interaction

import (
	"bufio"
	"os"
	"os/signal"
	"syscall"
	"time"

	"crdx.org/io/cmd/oh/key"
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
	Draw         func()
}

func Run(terminal *os.File, getNextRefresh func(time.Time) time.Time, handler Handler) {
	resizeSignals := resizes()
	defer signal.Stop(resizeSignals)

	refresh := newRefreshTimer(getNextRefresh)
	defer refresh.stop()

	beater := time.NewTicker(heartRate)
	defer beater.Stop()

	run(keypresses(terminal), resizeSignals, refresh.timer.C, refresh.schedule, beater.C, handler)
}

func run(keys <-chan key.Key, resizeSignals <-chan os.Signal, refreshes <-chan time.Time, schedule func(), beats <-chan time.Time, handler Handler) {
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
			settle(resizeSignals)
			handler.Resize()
		case <-beats:
			handler.Beat()

			select {
			case <-refreshes:
			default:
				continue
			}
		case <-refreshes:
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

func keypresses(terminal *os.File) <-chan key.Key {
	keys := make(chan key.Key)
	go func() {
		defer close(keys)
		decoder := key.NewDecoder(bufio.NewReader(terminal))
		for {
			keypress, err := decoder.Next()
			if err != nil {
				return
			}
			keys <- keypress
		}
	}()
	return keys
}

func resizes() chan os.Signal {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGWINCH)
	return signals
}

func settle(signals <-chan os.Signal) {
	time.Sleep(settling)
	for {
		select {
		case <-signals:
		default:
			return
		}
	}
}
