package output

// Group is a kind of output. Anything written in one group runs straight on from the last thing
// written in that same group, and a change of group opens a blank line between the two.
type Group int

// The groups the screen draws. They are peers: only equality decides whether output runs together.
const (
	NoticeGroup Group = iota
	WorkGroup
	AnswerGroup
)
