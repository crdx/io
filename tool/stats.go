package tool

import "context"

type statsCall struct {
	Call

	exec  func(context.Context) (string, Stats, error)
	stats Stats
	ran   bool
}

func (self *statsCall) Exec(ctx context.Context) (string, error) {
	output, stats, err := self.exec(ctx)
	self.stats = stats
	self.ran = true
	return output, err
}

func (self *statsCall) Stats() (Stats, bool) {
	return self.stats, self.ran
}
