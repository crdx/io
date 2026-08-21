package output

// Group is a kind of output. Anything written in one group runs straight on from the last thing
// written in that same group, and a change of group opens a blank line between the two, so a
// streamed answer never runs into a notice.
type Group int

// The groups the screen draws. There are two, and the rule does not care how many there are.
const (
	AsideGroup  Group = iota // anything set apart on a line of its own
	AnswerGroup              // prose streamed from the model
)
