package picker

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"crdx.org/io/cmd/oh/menu"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/internal/util"
	"crdx.org/io/internal/util/strutil"
	"crdx.org/io/session"
)

const (
	animalColumn      = 20
	modelColumn       = 20
	messageColumn     = 8
	lengthColumn      = 6
	lastMessageColumn = 12
	roomForModel      = 100
	markWidth         = 2
)

type Session struct {
	Name         string
	WorkspaceDir string
	Started      time.Time
	Touched      time.Time
	Title        string
	Model        string
	ModelID      string
	Effort       string
	MessageCount int
	IsRunning    bool
}

func (self *Session) Messages() int { return self.MessageCount }

func Choose(sessions []*Session, terminal *os.File, screen io.Writer) (*Session, error) {
	rows := &sessionList{sessions: sessions}

	chosen, err := menu.Choose(rows, terminal, screen)
	if err != nil {
		return nil, err
	}

	return sessions[chosen], nil
}

type sessionList struct {
	sessions []*Session
}

func (self *sessionList) Len() int { return len(self.sessions) }

func (self *sessionList) IsChoosable(index int) bool { return !self.sessions[index].IsRunning }

func (self *sessionList) Adjust(int, int) {}

func (self *sessionList) Text(index int) string {
	storedSession := self.sessions[index]

	return strings.Join([]string{
		storedSession.Name,
		storedSession.Title,
		storedSession.Model,
		storedSession.ModelID,
		storedSession.Effort,
	}, " ")
}

func (self *sessionList) ColumnHeader(room int) string {
	return menu.Columns(
		leftColumns(strings.Repeat(" ", markWidth), "Agent", "Title"),
		sessionColumns("Model", "Effort", "Messages", "Length", "Last Message", room),
		room,
	)
}

func (self *sessionList) Row(index int, isChosen bool, room int) string {
	storedSession := self.sessions[index]
	line := row(storedSession, isChosen, room)

	if storedSession.IsRunning {
		return style.Running(line)
	}
	if isChosen {
		return style.Chosen(line)
	}

	return style.Answer(line)
}

func row(storedSession *Session, isChosen bool, room int) string {
	prefix := menu.Mark(isChosen) + " "
	left := leftColumns(prefix, sessionAnimal(storedSession), sessionTitle(storedSession))
	return menu.Columns(
		left,
		sessionColumns(
			strutil.OrDash(storedSession.Model),
			storedSession.Effort,
			strconv.Itoa(storedSession.Messages()),
			util.CoarseDuration(storedSession.Touched.Sub(storedSession.Started)),
			util.Ago(storedSession.Touched),
			room,
		),
		room,
	)
}

func leftColumns(prefix string, animal string, title string) string {
	return prefix + menu.Pad(animal, animalColumn) + strings.Repeat(" ", menu.ColumnGap) + title
}

func sessionColumns(model string, effort string, messages string, length string, lastMessage string, room int) string {
	counted := fmt.Sprintf(
		"%*s  %*s  %*s",
		messageColumn,
		messages,
		lengthColumn,
		length,
		lastMessageColumn,
		lastMessage,
	)

	if room >= roomForModel {
		named := menu.Pad(model, modelColumn) +
			strings.Repeat(" ", menu.ColumnGap) +
			fmt.Sprintf("%*s", menu.EffortColumn, effort)

		counted = named + strings.Repeat(" ", menu.ColumnGap) + counted
	}

	return counted
}

func sessionAnimal(storedSession *Session) string {
	emoji := session.Emoji(storedSession.Name)
	if emoji == "" {
		return storedSession.Name
	}

	return emoji + " " + storedSession.Name
}

func sessionTitle(storedSession *Session) string {
	if storedSession.Title == "" {
		return "(untitled)"
	}

	return strings.ReplaceAll(storedSession.Title, "\n", " ")
}
