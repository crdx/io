package tool

import "context"

type measuredCall struct {
	Call

	exec  func(context.Context) (string, Statistics, error)
	stats Statistics
	ran   bool
}

func (self *measuredCall) Exec(ctx context.Context) (string, error) {
	output, stats, err := self.exec(ctx)
	self.stats = stats
	self.ran = true
	return output, err
}

func (self *measuredCall) Statistics() (Statistics, bool) {
	return self.stats, self.ran
}
