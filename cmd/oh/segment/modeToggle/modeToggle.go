package modeToggle

import (
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
)

type state struct {
	getGrantedCaps  func() caps.Set
	isPrefixPending func() bool
}

func New(getGrantedCaps func() caps.Set, isPrefixPending func() bool) segment.Factory {
	return func(segment.Options) (segment.Segment, error) {
		return state{
			getGrantedCaps:  getGrantedCaps,
			isPrefixPending: isPrefixPending,
		}, nil
	}
}

func (self state) Render(segment.Context) string {
	grantedCaps := self.getGrantedCaps()
	isPrefixPending := self.isPrefixPending()

	return self.letter(caps.Read, true, style.Read, isPrefixPending) +
		self.letter(caps.Shell, grantedCaps.Has(caps.Shell), style.Exec, isPrefixPending) +
		self.letter(caps.Write, grantedCaps.Has(caps.Write), style.Write, isPrefixPending) +
		self.letter(caps.Git, grantedCaps.Has(caps.Git), style.History, isPrefixPending) +
		self.letter(caps.Background, grantedCaps.Has(caps.Background), style.Background, isPrefixPending)
}

func (self state) letter(caps caps.Set, isGranted bool, paint style.Style, isPrefixPending bool) string {
	if !isGranted {
		paint = style.Withheld
	}

	if isPrefixPending {
		return style.Pending(paint(caps.Flag()))
	}

	return paint(caps.Flag())
}
