package turn

type Kind int

const (
	None Kind = iota
	Replacement
	ModeChange
	ModeNotice
)

type Pending struct {
	Message     string
	Replacement bool
	ModeChange  bool
	ModeNotice  bool
}

type Queue struct {
	pending Pending
}

func (self *Queue) Replace(message string) {
	self.pending.Message = message
	self.pending.Replacement = true
}

func (self *Queue) MarkModeChange() {
	self.pending.ModeChange = true
}

func (self *Queue) Clear() {
	self.pending = Pending{}
}

func (self *Queue) Drop() {
	self.pending = Pending{ModeNotice: self.pending.ModeChange || self.pending.ModeNotice}
}

func (self *Queue) Empty() bool {
	return !self.pending.Replacement && !self.pending.ModeChange && !self.pending.ModeNotice
}

func (self *Queue) Peek() Pending {
	return self.pending
}

func (self *Queue) Take() (Kind, string) {
	pending := self.pending
	self.Clear()

	switch {
	case pending.Replacement:
		return Replacement, pending.Message
	case pending.ModeChange:
		return ModeChange, ""
	case pending.ModeNotice:
		return ModeNotice, ""
	default:
		return None, ""
	}
}
