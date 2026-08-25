package painter

import (
	"slices"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/width"
)

type ModeNotices struct {
	events []agent.Event
}

func NewModeNotices(events []agent.Event) *ModeNotices {
	return &ModeNotices{events: slices.Clone(events)}
}

func (self *ModeNotices) ReplaceEvents(events []agent.Event) {
	self.events = slices.Clone(events)
}

func (self *ModeNotices) Rows(columns int) []string {
	var rows []string

	for _, event := range self.events {
		notice, said := renderModeNotice(event)
		if said {
			rows = append(rows, width.Wrap(notice, columns)...)
		}
	}

	return rows
}

func renderModeNotice(event agent.Event) (string, bool) {
	notice, said := caps.ModeNotice(event)
	if !said {
		return "", false
	}

	return NoticeStyle(agent.WarningStatus)(notice), true
}
