package activeModel

import (
	"slices"
	"strings"

	"crdx.org/io/cmd/oh/model"
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
)

const (
	filledSquare      = "▪"
	emptySquare       = "▫"
	unsupportedSquare = "·"
)

var modelDisplayNames = map[string][]string{
	"gpt-5.3-codex":                {"Codex", "5.3"},
	"gpt-5.3-codex-spark":          {"Codex Spark", "5.3"},
	"gpt-5.4-mini":                 {"GPT Mini", "5.4"},
	"gpt-5.4-nano":                 {"GPT Nano", "5.4"},
	"gpt-5.5-pro":                  {"GPT Pro", "5.5"},
	"gpt-5.6":                      {"GPT", "5.6"},
	"gpt-5.6-luna":                 {"GPT Luna", "5.6"},
	"gpt-5.6-sol":                  {"GPT Sol", "5.6"},
	"gpt-5.6-terra":                {"GPT Terra", "5.6"},
	"o3":                           {"o3"},
	"o3-pro":                       {"o3 Pro"},
	"o4-mini":                      {"o4 Mini"},
	"kimi-k3":                      {"Kimi", "K3"},
	"kimi-k2.7-code":               {"Kimi Code", "K2.7"},
	"longcat-2.0":                  {"LongCat", "2.0"},
	"glm-5.3-flash":                {"GLM Flash", "5.3"},
	"glm-5.3":                      {"GLM", "5.3"},
	"deepseek-v4-pro":              {"DeepSeek Pro", "4"},
	"deepseek-v4-flash":            {"DeepSeek Flash", "4"},
	"deepseek-v4-flash-vision-exp": {"DeepSeek Flash Vision Exp", "4"},
	"mimo-v2-omni":                 {"Mimo Omni", "v2"},
	"mimo-v2.5-pro":                {"Mimo Pro", "v2.5"},
	"mimo-v2.5":                    {"Mimo", "v2.5"},
	"hy3":                          {"HY3"},
	"claude-opus-5":                {"Opus", "5"},
	"claude-sonnet-5":              {"Sonnet", "5"},
	"claude-fable-5":               {"Fable", "5"},
}

type state struct {
	name         string
	effort       string
	effortLevels []string
}

func New(name string, effort string, effortLevels []string) segment.Factory {
	return func(segment.Options) (segment.Segment, error) {
		return state{name: name, effort: effort, effortLevels: effortLevels}, nil
	}
}

func (self state) Render(segment.Context) string {
	name := displayName(self.name)
	badge := style.Normal(name[0])
	if len(name) > 1 {
		badge += " " + style.Subtle(name[1])
	}

	squares := thinkingSquares(self.effort, self.effortLevels)
	if squares == "" {
		return badge
	}

	return badge + " " + styleThinkingSquares(squares)
}

func displayName(model string) []string {
	clean := model
	if colon := strings.LastIndex(clean, ":"); colon >= 0 {
		clean = clean[:colon]
	}
	if slash := strings.LastIndex(clean, "/"); slash >= 0 {
		clean = clean[slash+1:]
	}
	if displayName, found := modelDisplayNames[clean]; found {
		return displayName
	}
	return []string{clean}
}

func thinkingSquares(effort string, effortLevels []string) string {
	if len(effortLevels) == 0 {
		return ""
	}

	var squares strings.Builder

	for _, level := range model.EffortOrder[1:] {
		switch {
		case !slices.Contains(effortLevels, level):
			squares.WriteString(unsupportedSquare)
		case level == effort:
			squares.WriteString(filledSquare)
		default:
			squares.WriteString(emptySquare)
		}
	}

	return squares.String()
}

func styleThinkingSquares(squares string) string {
	var rendered strings.Builder
	var subtle strings.Builder

	flushSubtle := func() {
		if subtle.Len() > 0 {
			rendered.WriteString(style.Subtle(subtle.String()))
			subtle.Reset()
		}
	}

	for _, square := range squares {
		if string(square) == filledSquare {
			flushSubtle()
			rendered.WriteString(style.Chosen(string(square)))
		} else {
			subtle.WriteRune(square)
		}
	}
	flushSubtle()

	return rendered.String()
}
