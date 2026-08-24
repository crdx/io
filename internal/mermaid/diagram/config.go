package diagram

// Config controls the terminal rendering choices used by the supported diagrams.
type Config struct {
	UseAscii bool

	BoxBorderPadding int

	PaddingBetweenX int

	PaddingBetweenY int

	StyleType string

	SequenceParticipantSpacing int

	SequenceMessageSpacing int

	SequenceSelfMessageWidth int
}

// DefaultConfig returns the terminal renderer's defaults.
func DefaultConfig() *Config {
	return &Config{
		UseAscii:                   false,
		BoxBorderPadding:           1,
		PaddingBetweenX:            5,
		PaddingBetweenY:            5,
		StyleType:                  "cli",
		SequenceParticipantSpacing: 5,
		SequenceMessageSpacing:     1,
		SequenceSelfMessageWidth:   4,
	}
}
