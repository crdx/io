package turn

type Kind int

const (
	None Kind = iota
	Replacement
	AccessChange
	AccessNotice
	Poke
)

type Pending struct {
	Message      string
	Replacement  bool
	AccessChange bool
	AccessNotice bool
	Poke         bool
}

type Queue struct {
	pending Pending
}

func (self *Queue) Replace(message string) {
	self.pending.Message = message
	self.pending.Replacement = true
}

func (self *Queue) MarkAccessChange() {
	self.pending.AccessChange = true
}

func (self *Queue) MarkSilentTurn() {
	self.pending.Poke = true
}

func (self *Queue) Clear() {
	self.pending = Pending{}
}

func (self *Queue) Drop() {
	self.pending = Pending{AccessNotice: self.pending.AccessChange || self.pending.AccessNotice}
}

func (self *Queue) Empty() bool {
	return !self.pending.Replacement && !self.pending.AccessChange &&
		!self.pending.AccessNotice && !self.pending.Poke
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
	case pending.AccessChange:
		return AccessChange, ""
	case pending.AccessNotice:
		return AccessNotice, ""
	case pending.Poke:
		return Poke, PokeMessage
	default:
		return None, ""
	}
}
