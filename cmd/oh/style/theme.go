package style

import (
	"fmt"
	"image/color"
	"strings"
	"sync/atomic"
)

const defaultColour = "default"

type Colour string

func (self *Colour) UnmarshalText(text []byte) error {
	value := strings.ToLower(strings.TrimSpace(string(text)))
	if value == defaultColour {
		*self = ""
		return nil
	}
	if _, isColour := colour(value); !isColour {
		return fmt.Errorf("%q is not a colour; write #rrggbb or %q", text, defaultColour)
	}
	*self = Colour(value)
	return nil
}

type Theme struct {
	Normal         Colour `toml:"normal"`
	Dim            Colour `toml:"dim"`
	Accent         Colour `toml:"accent"`
	StatusSuccess  Colour `toml:"status_success"`
	StatusInfo     Colour `toml:"status_info"`
	StatusWarning  Colour `toml:"status_warning"`
	StatusDanger   Colour `toml:"status_danger"`
	SyntaxType     Colour `toml:"syntax_type"`
	SyntaxLiteral  Colour `toml:"syntax_literal"`
	SyntaxOperator Colour `toml:"syntax_operator"`
	User           Colour `toml:"user"`
	Harness        Colour `toml:"harness"`
}

var defaultTheme = Theme{
	Normal:         "",
	Dim:            "#969896",
	Accent:         "#c08050",
	StatusSuccess:  "#4c9a2c",
	StatusInfo:     "#81a2be",
	StatusWarning:  "#cfad00",
	StatusDanger:   "#cc6666",
	SyntaxType:     "#f0c674",
	SyntaxLiteral:  "#b5bd68",
	SyntaxOperator: "#8abeb7",
	User:           "#343541",
	Harness:        "#303a43",
}

func DefaultTheme() Theme {
	return defaultTheme
}

type compiledTheme struct {
	normal         string
	dim            string
	accent         string
	statusWarning  string
	statusSuccess  string
	statusInfo     string
	statusDanger   string
	syntaxType     string
	syntaxLiteral  string
	syntaxOperator string
	user           string
	harness        string

	dimColour           color.RGBA
	statusWarningColour color.RGBA
	statusInfoColour    color.RGBA
	statusDangerColour  color.RGBA
}

var activeTheme = newAtomicTheme(defaultTheme)

func newAtomicTheme(theme Theme) *atomic.Pointer[compiledTheme] {
	active := &atomic.Pointer[compiledTheme]{}
	active.Store(compileTheme(theme))
	return active
}

func ApplyTheme(theme Theme) func() {
	previous := activeTheme.Swap(compileTheme(theme))

	return func() { activeTheme.Store(previous) }
}

func compileTheme(theme Theme) *compiledTheme {
	dimColour := graphicColour(theme.Dim, defaultTheme.Dim)
	statusWarningColour := graphicColour(theme.StatusWarning, defaultTheme.StatusWarning)
	statusInfoColour := graphicColour(theme.StatusInfo, defaultTheme.StatusInfo)
	statusDangerColour := graphicColour(theme.StatusDanger, defaultTheme.StatusDanger)

	return &compiledTheme{
		normal:              sgr(string(theme.Normal)),
		dim:                 sgr(string(theme.Dim)),
		accent:              sgr(string(theme.Accent)),
		statusWarning:       sgr(string(theme.StatusWarning)),
		statusSuccess:       sgr(string(theme.StatusSuccess)),
		statusInfo:          sgr(string(theme.StatusInfo)),
		statusDanger:        sgr(string(theme.StatusDanger)),
		syntaxType:          sgr(string(theme.SyntaxType)),
		syntaxLiteral:       sgr(string(theme.SyntaxLiteral)),
		syntaxOperator:      sgr(string(theme.SyntaxOperator)),
		user:                backgroundSequence(string(theme.User)),
		harness:             backgroundSequence(string(theme.Harness)),
		dimColour:           dimColour,
		statusWarningColour: statusWarningColour,
		statusInfoColour:    statusInfoColour,
		statusDangerColour:  statusDangerColour,
	}
}

func graphicColour(value Colour, fallback Colour) color.RGBA {
	if parsedColour, isColour := colour(string(value)); isColour {
		return parsedColour
	}
	parsedColour, _ := colour(string(fallback))
	return parsedColour
}

func DimColour() color.RGBA {
	return activeTheme.Load().dimColour
}

func ChangeColour() color.RGBA {
	return activeTheme.Load().statusWarningColour
}

func InformationColour() color.RGBA {
	return activeTheme.Load().statusInfoColour
}

func FailureColour() color.RGBA {
	return activeTheme.Load().statusDangerColour
}
