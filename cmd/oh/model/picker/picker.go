package picker

import (
	"io"
	"os"
	"slices"
	"strings"

	"crdx.org/io/cmd/oh/menu"
	"crdx.org/io/cmd/oh/segment/fastMode"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/table"
	"crdx.org/io/internal/util"
)

const (
	markWidth        = 2
	providerColumn   = 12
	nameColumn       = 28
	effortColumn     = 9
	contextColumn    = 7
	identifierColumn = 28
)

type Effort struct {
	Level  string
	IsFast bool
}

func (self Effort) String() string {
	if self.IsFast {
		return self.Level + " " + fastMode.FastMark
	}

	return self.Level
}

type Model struct {
	Provider            string
	ProviderID          string
	Name                string
	ID                  string
	EffortLevels        []Effort
	Effort              Effort
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
	return modelTable().Header(room)
}

func (self *modelList) Row(index int, isChosen bool, room int) string {
	paint := style.Answer
	if isChosen {
		paint = style.ChosenRow
	}

	return paint.Over(modelRow(self.models[index], isChosen, room))
}

func modelTable() *table.Table {
	return table.New(
		table.Column{Title: "  Provider", Width: markWidth + providerColumn},
		table.Column{Title: "Model", Width: nameColumn},
		table.Column{Title: "Effort", Width: effortColumn},
		table.Column{Title: "Context", Width: contextColumn, Align: table.Right},
		table.Column{Title: "Identifier", Width: identifierColumn, Style: style.Subtle},
	)
}

func modelRow(model *Model, isChosen bool, room int) string {
	return modelTable().Row([]string{
		menu.Mark(isChosen) + " " + model.Provider,
		model.Name,
		model.Effort.String(),
		contextWindow(model.ContextWindowTokens),
		model.ID,
	}, room)
}

func contextWindow(tokens int) string {
	if tokens <= 0 {
		return "—"
	}

	return util.FormatTokenCount(tokens)
}
