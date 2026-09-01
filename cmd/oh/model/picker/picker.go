package picker

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"crdx.org/io/cmd/oh/menu"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/internal/util"
)

const (
	providerColumn   = 12
	nameColumn       = 28
	contextColumn    = 7
	identifierColumn = 28
)

type Model struct {
	Provider            string
	ProviderID          string
	Name                string
	ID                  string
	EffortLevels        []string
	Effort              string
	ContextWindowTokens int
}

func Choose(models []*Model, terminal *os.File, screen io.Writer) (*Model, error) {
	chosenIndex, err := menu.Choose(&modelList{models: models}, terminal, screen)
	if err != nil {
		return nil, err
	}

	return models[chosenIndex], nil
}

type modelList struct {
	models []*Model
}

func (self *modelList) Len() int { return len(self.models) }

func (self *modelList) IsChoosable(int) bool { return true }

func (self *modelList) Text(index int) string {
	model := self.models[index]

	return strings.Join([]string{model.Provider, model.ProviderID, model.Name, model.ID}, " ")
}

func (self *modelList) Adjust(index int, direction int) {
	model := self.models[index]

	at := slices.Index(model.EffortLevels, model.Effort)
	if at < 0 {
		return
	}

	if wantedIndex := at + direction; wantedIndex >= 0 && wantedIndex < len(model.EffortLevels) {
		model.Effort = model.EffortLevels[wantedIndex]
	}
}

func (self *modelList) ColumnHeader(room int) string {
	describedModel, identifier := modelColumns(" ", "Provider", "Model", "Effort", "Context", "Identifier", room)

	return describedModel + identifier
}

func (self *modelList) Row(index int, isChosen bool, room int) string {
	describedModel, identifier := modelRow(self.models[index], isChosen, room)

	paint := style.Answer
	if isChosen {
		paint = style.ChosenRow
	}
	if identifier == "" {
		return paint(describedModel)
	}

	return paint(describedModel) + style.Subtle(identifier)
}

func modelRow(model *Model, isChosen bool, room int) (string, string) {
	return modelColumns(
		menu.Mark(isChosen),
		model.Provider,
		model.Name,
		model.Effort,
		contextWindow(model.ContextWindowTokens),
		model.ID,
		room,
	)
}

func modelColumns(
	prefix string,
	providerName string,
	name string,
	effort string,
	context string,
	identifier string,
	room int,
) (string, string) {
	columns := []string{
		menu.Pad(providerName, providerColumn),
		menu.Pad(name, nameColumn),
		menu.Pad(effort, menu.EffortColumn),
		fmt.Sprintf("%*s", contextColumn, context),
	}

	gap := strings.Repeat(" ", menu.ColumnGap)
	describedRow := menu.Clip(prefix+" "+strings.Join(columns, gap)+gap, room)

	return describedRow, menu.Clip(identifier, min(identifierColumn, room-style.Width(describedRow)))
}

func contextWindow(tokens int) string {
	if tokens <= 0 {
		return "—"
	}

	return util.FormatTokenCount(tokens)
}
