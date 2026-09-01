package diagram

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
