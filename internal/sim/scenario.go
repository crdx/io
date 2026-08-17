package sim

import (
	"fmt"
	"time"

	"github.com/BurntSushi/toml"
)

// Scenario is what the endpoint pretends to be, read from a file. Every new session starts at the
// first turn and takes the next one with each request it makes.
type Scenario struct {
	Model string `toml:"model"` // the model name reported

	Loop bool `toml:"loop"` // whether turns repeat

	Strict bool `toml:"strict"` // whether requests are checked

	Pace  Duration `toml:"pace"`  // the delay between events
	Delay Duration `toml:"delay"` // the delay before a response

	Turns []Turn `toml:"turn"` // the answers in order
}

// Turn is one request answered: what the model thinks, says, and asks to be run, in that order.
type Turn struct {
	Think []string `toml:"think"` // the reasoning pieces
	Say   string   `toml:"say"`   // the answer
	Calls []Call   `toml:"call"`  // the tool calls

	Pace  Duration `toml:"pace"`  // the delay between events
	Delay Duration `toml:"delay"` // the delay before responding

	Fail       string `toml:"fail"`        // the stream failure
	Incomplete bool   `toml:"incomplete"`  // whether the response is incomplete
	ErrorEvent string `toml:"error-event"` // a raw error event

	Truncate bool `toml:"truncate"` // whether the stream ends abruptly

	Status int `toml:"status"` // the HTTP status
}

// Call is a tool the model asks for.
type Call struct {
	Name      string `toml:"name"`      // which tool is called
	Arguments string `toml:"arguments"` // what it is called with
}

// Duration is a length of time written the way Go writes one, as in "150ms".
type Duration struct {
	time.Duration // the parsed duration
}

// UnmarshalText reads a duration from the file.
func (self *Duration) UnmarshalText(text []byte) error {
	durationValue, err := time.ParseDuration(string(text))
	if err != nil {
		return err
	}

	self.Duration = durationValue

	return nil
}

// Read loads a scenario, and says what is wrong with it rather than pretending it is fine.
func Read(path string) (*Scenario, error) {
	var self Scenario

	metadata, err := toml.DecodeFile(path, &self)
	if err != nil {
		return nil, err
	}

	if undecodedKeys := metadata.Undecoded(); len(undecodedKeys) > 0 {
		return nil, fmt.Errorf("%s: no such setting: %s", path, undecodedKeys[0])
	}

	if len(self.Turns) == 0 {
		return nil, fmt.Errorf("%s: a scenario needs at least one turn", path)
	}

	if self.Model == "" {
		self.Model = "fake"
	}

	return &self, nil
}

func (self *Scenario) turn(index int) (Turn, bool) {
	if index < len(self.Turns) {
		return self.Turns[index], true
	}

	if self.Loop && len(self.Turns) > 0 {
		return self.Turns[index%len(self.Turns)], true
	}

	return Turn{}, false
}

func (self *Scenario) pace(turn Turn) time.Duration {
	if turn.Pace.Duration > 0 {
		return turn.Pace.Duration
	}

	return self.Pace.Duration
}

func (self *Scenario) delay(turn Turn) time.Duration {
	if turn.Delay.Duration > 0 {
		return turn.Delay.Duration
	}

	return self.Delay.Duration
}
