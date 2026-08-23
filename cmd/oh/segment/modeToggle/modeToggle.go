package modeToggle

import (
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
)

type state struct {
	getGrantedCaps func() caps.Set
	isChordPending func() bool
}

func New(getGrantedCaps func() caps.Set, isChordPending func() bool) segment.Factory {
	return func(segment.Options) (segment.Segment, error) {
		return state{
			getGrantedCaps: getGrantedCaps,
			isChordPending: isChordPending,
		}, nil
	}
}

func (self state) Render(segment.Context) string {
	grantedCaps := self.getGrantedCaps()
	isChordPending := self.isChordPending()

	return self.letter(caps.Read, true, style.Read, isChordPending) +
		self.letter(caps.Shell, grantedCaps.Has(caps.Shell), style.Exec, isChordPending) +
		self.letter(caps.Write, grantedCaps.Has(caps.Write), style.Write, isChordPending) +
		self.letter(caps.Git, grantedCaps.Has(caps.Git), style.History, isChordPending) +
		self.letter(caps.Background, grantedCaps.Has(caps.Background), style.Background, isChordPending)
}

func (self state) letter(caps caps.Set, isGranted bool, paint style.Style, isChordPending bool) string {
	if !isGranted {
		paint = style.Withheld
	}

	if isChordPending {
		return style.Pending(paint(caps.Flag()))
	}

	return paint(caps.Flag())
}
