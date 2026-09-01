package diagram

type Diagram interface {
	Parse(input string) error
	Render(config *Config) (string, error)
	Type() string
}
