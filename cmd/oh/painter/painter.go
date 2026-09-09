package painter

import (
	"strconv"
	"strings"

	"crdx.org/io/agent"
	"crdx.org/io/internal/toolresult"
	"crdx.org/io/internal/util"
	"crdx.org/io/internal/util/strutil"
	"crdx.org/io/tool"

	"crdx.org/io/cmd/oh/call"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/dynamic"
	"crdx.org/io/cmd/oh/interrupt"
	"crdx.org/io/cmd/oh/jobrecord"
	"crdx.org/io/cmd/oh/link"
	"crdx.org/io/cmd/oh/markdown"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/pathgrant"
	"crdx.org/io/cmd/oh/pictures"
	"crdx.org/io/cmd/oh/startup"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/turn"
	"crdx.org/io/cmd/oh/width"
	"crdx.org/io/cmd/oh/work"
)

const retryArgumentsCells = 120

type Picasso struct {
	screen         *output.Screen
	toolBlock      *dynamic.Block
	rows           map[string]int
	labels         map[string]call.Label
	answer         liveText
	answerRenderer markdown.IncrementalRenderer
	reasoning      liveText
	previousKind   agent.Kind

	isStale               bool
	isRunning             bool
	streamingMode         output.StreamingMode
	reasoningRendering    output.ReasoningRendering
	resultLinkSessionName string

	getTool   func(string) (tool.Tool, bool)
	workspace *work.Space
	pictures  pictureSource
}

type pictureSource struct {
	sessionDirectory string
	cellWidth        int
	cellHeight       int
	isLocal          bool
}

func New(
	screen *output.Screen,
	isRunning bool,
	getTool func(string) (tool.Tool, bool),
	workspace *work.Space,
	streamingMode output.StreamingMode,
) *Picasso {
	self := &Picasso{
		screen:        screen,
		isRunning:     isRunning,
		getTool:       getTool,
		workspace:     workspace,
		streamingMode: streamingMode,
	}

	self.answer.streamingMode = streamingMode
	self.reasoning.streamingMode = streamingMode

	return self
}

func (self *Picasso) RenderReasoningAs(rendering output.ReasoningRendering) {
	self.reasoningRendering = rendering
}

func (self *Picasso) DrawPicturesFrom(sessionDirectory string, cellWidth int, cellHeight int, isLocal bool) {
	self.pictures = pictureSource{
		sessionDirectory: sessionDirectory,
		cellWidth:        cellWidth,
		cellHeight:       cellHeight,
		isLocal:          isLocal,
	}
}

func (self *Picasso) LinkToolResults(sessionName string) {
	self.resultLinkSessionName = sessionName
}

func (self *Picasso) DrawDelta(delta agent.Delta) {
	self.drawDeltaWithAnswerRendererReset(delta, true)
}

func (self *Picasso) DrawRestoredDelta(delta agent.Delta, previous *Picasso) {
	self.answerRenderer = previous.answerRenderer
	self.drawDeltaWithAnswerRendererReset(delta, false)
}

func (self *Picasso) DrawEvent(event agent.Event) {
	switch {
	case event.Kind == agent.ModelReasoningEvent && self.previousKind == agent.ModelReasoningEvent && self.reasoning.Len() == 0:
		self.screen.End()
	case event.Kind == agent.ModelMessageEvent && self.previousKind == agent.ModelMessageEvent && self.answer.Len() == 0:
		self.screen.Blank()
	}
	self.previousKind = event.Kind

	if event.Kind != agent.ModelReasoningEvent && event.Kind != agent.ModelMessageEvent {
		self.discardProvisionalReasoning()
		self.settleAnswer()
		self.answer.Reset()
	}

	switch event.Kind {
	case agent.UserMessageEvent:
		self.discardProvisionalReasoning()
		self.answer.Reset()
		self.drawSubmitted(event.Text, "")

	case agent.ModelReasoningEvent:
		self.answer.Reset()
		self.reasoning.Reset()
		self.reasoning.Write(event.Text)
		self.drawReasoning(true)
		self.screen.Seal()
		self.reasoning.Reset()

	case agent.ModelMessageEvent:
		self.discardProvisionalReasoning()
		self.answer.Reset()
		self.answer.Write(event.Text)
		var renderedAnswer []string
		if self.screen.IsTerminal() {
			renderedAnswer = markdown.RenderWithHyperlinksUnder(
				self.answer.Text(),
				self.screen.Columns(),
				self.workspace.GetDir(),
			)
		} else {
			renderedAnswer = markdown.Render(self.answer.Text(), self.screen.Columns())
		}
		if !self.screen.DrawAnswer(renderedAnswer) {
			self.isStale = true
		}
		self.screen.Seal()
		self.answer.Reset()

	case agent.ToolCallRequestEvent:
		if self.toolBlock == nil {
			self.toolBlock = dynamic.NewBlock(self.screen.Refresh)
			self.screen.Open(self.toolBlock)
			self.rows = map[string]int{}
			self.labels = map[string]call.Label{}
		}

		label := call.LabelFor(event, self.getTool, self.workspace)
		self.rows[event.ID] = self.toolBlock.Add(label)
		self.labels[event.ID] = label

	case agent.ToolCallResultEvent:
		self.mark(event)

	case agent.StartupEvent:
		self.screen.Line(self.render(event))

	case agent.SilentTurnEvent:
		self.screen.Line(style.StoppedTurn(agent.SilentTurnNotice))

	case agent.PrefixRewriteEvent:
		self.screen.Line(style.Failure(agent.PrefixRewriteNotice + event.Text))

	case agent.CacheRebuildEvent:
		self.screen.Line(style.Change(agent.CacheRebuildNotice(event)))

	case caps.ModeChange, caps.JobStop, jobrecord.Ended, jobrecord.EndedWithSession, pathgrant.Change,
		turn.HarnessPoke:
		if message, isSaid := HarnessNotice(event); isSaid {
			self.drawSubmitted(message, submittedMarker(true))
		}

	case agent.RetryingEvent:
		self.Close(dynamic.Cancelled)
		self.screen.Line(style.StoppedTurn(RenderRetry(event)))

	case agent.FailureEvent:
		self.Close(dynamic.Cancelled)
		self.screen.Line(style.Failure(RenderFailure(event)))

	case agent.InterruptionEvent:
		if interrupt.IsAnnounced(event) {
			self.Close(dynamic.Cancelled)
			self.screen.Line(style.StoppedTurn(interrupt.Notice(event)))
		}

	case agent.StateChangeEvent:
	}
}

func (self *Picasso) ProvisionalDelta() agent.Delta {
	if self.reasoning.Len() > 0 {
		return agent.Delta{Kind: agent.ModelReasoningEvent, Text: self.reasoning.String()}
	}

	return agent.Delta{Kind: agent.ModelMessageEvent, Text: self.answer.String()}
}

func RenderRetry(event agent.Event) string {
	notice := "[#" + strconv.Itoa(event.Attempt) + "] Request failed"

	if event.Took > 0 {
		notice += "; retrying in " + util.CompactDuration(event.Took)
	} else {
		notice += "; retrying"
	}

	if event.Text != "" {
		notice += ": " + strutil.Capitalise(strutil.Flatten(strutil.FirstLine(event.Text)))
	}

	if event.Arguments != "" {
		notice += ": " + width.Elide(strutil.Flatten(event.Arguments), retryArgumentsCells)
	}

	return notice
}

func RenderSubmittedMessage(text string, columns int) string {
	return renderSubmittedMessage(text, columns, false, "", "")
}

func RenderSubmittedMessageWithHyperlinks(text string, columns int) string {
	return renderSubmittedMessage(text, columns, true, "", "")
}

func renderSubmittedMessage(text string, columns int, shouldRenderHyperlinks bool, workspace string, marker string) string {
	contentColumns := columns
	if contentColumns > 1 {
		contentColumns--
	}

	markerWidth := width.Of(marker)
	if markerWidth > 0 && contentColumns > markerWidth {
		contentColumns -= markerWidth
	}

	var content []string
	if shouldRenderHyperlinks {
		content = markdown.RenderWithHyperlinksUnder(strutil.StripControl(text), contentColumns, workspace)
	} else {
		content = markdown.Render(strutil.StripControl(text), contentColumns)
	}
	for i, row := range content {
		if shouldRenderHyperlinks && workspace != "" {
			row = link.Render(row, workspace)
		}

		prefix := " "
		switch {
		case marker == "":
		case i == 0:
			prefix += marker
		default:
			prefix += strings.Repeat(" ", markerWidth)
		}
		content[i] = prefix + row
	}

	rows := append([]string{""}, content...)
	rows = append(rows, "")

	for i, row := range rows {
		if room := columns - style.Width(row); room > 0 {
			row += strings.Repeat(" ", room)
		}

		rows[i] = style.User(row)
	}

	return strings.Join(rows, "\n")
}

func NoticeStyle(severity agent.Status) style.Style {
	switch severity {
	case agent.InfoStatus:
		return style.Information
	case agent.SuccessStatus:
		return style.Success
	case agent.ErrorStatus:
		return style.Failure
	case agent.CancelledStatus:
		return style.CancelledCall
	case agent.WarningStatus, "":
		return style.StoppedTurn
	default:
		return style.Normal
	}
}

func getState(status agent.Status) dynamic.RowState {
	switch status {
	case agent.ErrorStatus:
		return dynamic.Failed
	case agent.CancelledStatus:
		return dynamic.Cancelled
	case agent.InfoStatus, agent.SuccessStatus, agent.WarningStatus:
		return dynamic.Done
	default:
		return dynamic.Done
	}
}

func RenderReasoning(thought string, columns int, rendering output.ReasoningRendering) []string {
	renderedRows := markdown.Render(thought, columns)

	if rendering == output.ReasoningMarkdown {
		for i, row := range renderedRows {
			renderedRows[i] = style.Reasoning.Over(row)
		}

		return renderedRows
	}

	plain := style.Plain(strings.Join(renderedRows, "\n"))
	strippedText := strings.Join(strings.Fields(plain), " ")

	return width.Wrap(style.Reasoning(strippedText), columns)
}

func (self *Picasso) Stale() bool { return self.isStale }

func (self *Picasso) Close(state dynamic.RowState) {
	self.discardProvisionalReasoning()
	self.settleAnswer()

	if self.toolBlock != nil {
		self.toolBlock.Close(state)
		self.toolBlock = nil
		self.rows = nil
		self.labels = nil

		self.screen.Seal()
	}
}

func (self *Picasso) End() {
	self.screen.End()
}

func (self *Picasso) Stop() {
	if self.toolBlock != nil {
		self.toolBlock.Stop()
		self.toolBlock = nil
		self.rows = nil
		self.labels = nil

		self.screen.Seal()
	}
}

func (self *Picasso) drawSubmitted(text string, marker string) {
	self.Close(dynamic.Cancelled)
	self.screen.Blank()
	self.screen.Line(self.renderUserMessage(text, marker))
	self.screen.End()
	self.screen.Blank()
}

func (self *Picasso) renderUserMessage(text string, marker string) string {
	if !self.screen.IsTerminal() {
		return renderSubmittedMessage(text, self.screen.Columns(), false, "", marker)
	}

	return renderSubmittedMessage(text, self.screen.Columns(), true, self.workspace.GetDir(), marker)
}

func (self *Picasso) drawDeltaWithAnswerRendererReset(delta agent.Delta, shouldResetAnswerRenderer bool) {
	switch delta.Kind { //nolint:exhaustive // Only model prose event kinds can be deltas.
	case agent.ModelReasoningEvent:
		if self.reasoning.Len() == 0 && self.previousKind == agent.ModelReasoningEvent {
			self.screen.End()
		}
		self.reasoning.Write(delta.Text)
		if self.reasoning.IsDue() {
			self.drawReasoning(false)
		}

	case agent.ModelMessageEvent:
		self.discardProvisionalReasoning()
		if self.answer.Len() == 0 {
			if shouldResetAnswerRenderer {
				self.answerRenderer.Reset()
			}
			if self.previousKind == agent.ModelMessageEvent {
				self.screen.Blank()
			}
		}
		self.answer.Write(delta.Text)
		if self.answer.IsDue() {
			self.drawAnswer(false)
		}
	}
}

func (self *Picasso) settleAnswer() {
	if self.answer.IsOwed() {
		self.drawAnswer(true)
	}
}

func (self *Picasso) drawReasoning(isSettled bool) {
	thought, isRowArriving := self.withoutArrivingTableRow(self.reasoning.Text(), isSettled)
	rows := RenderReasoning(thought, self.screen.Columns(), self.reasoningRendering)

	isTailHidden := !isSettled && self.streamingMode == output.StreamingModeLine
	if isTailHidden {
		rows = self.reasoning.WithoutLastRow(rows)
	}

	if !self.screen.DrawReasoning(self.reasoning.Take(rows, isTailHidden || isRowArriving)) {
		self.isStale = true
	}
}

func (self *Picasso) drawAnswer(isSettled bool) {
	answerText, isRowArriving := self.withoutArrivingTableRow(self.answer.Text(), isSettled)

	var rows []string
	if self.screen.IsTerminal() {
		rows = self.answerRenderer.RenderWithHyperlinksUnder(
			answerText,
			self.screen.Columns(),
			self.workspace.GetDir(),
		)
	} else {
		rows = self.answerRenderer.Render(answerText, self.screen.Columns())
	}

	isTailHeldBack := !isRowArriving && self.isTailHeldBack(isSettled)
	if isTailHeldBack {
		rows = self.answer.WithoutLastRow(rows)
	}

	if !self.screen.DrawAnswer(self.answer.Take(rows, isTailHeldBack || isRowArriving)) {
		self.isStale = true
	}
}

func (self *Picasso) isTailHeldBack(isSettled bool) bool {
	if isSettled || self.streamingMode != output.StreamingModeLine {
		return false
	}

	return !strings.HasSuffix(self.answer.String(), "\n") && !self.answerRenderer.IsTailMermaid()
}

func (self *Picasso) withoutArrivingTableRow(text string, isSettled bool) (string, bool) {
	if isSettled || self.streamingMode != output.StreamingModeLine {
		return text, false
	}

	settledEnd := strings.LastIndex(text, "\n") + 1
	arrivingRow := text[settledEnd:]

	if !strings.Contains(arrivingRow, "|") || !markdown.EndsWithTable(text[:settledEnd]) {
		return text, false
	}

	return text[:settledEnd], true
}

func (self *Picasso) discardProvisionalReasoning() {
	if self.reasoning.Len() == 0 {
		return
	}

	if !self.screen.DiscardLive() {
		self.isStale = true
	}
	self.reasoning.Reset()
}

func (self *Picasso) mark(event agent.Event) {
	if self.toolBlock == nil {
		return
	}

	index, isKnown := self.rows[event.ID]
	if !isKnown {
		return
	}

	delete(self.rows, event.ID)
	label := self.labels[event.ID]
	delete(self.labels, event.ID)
	if self.resultLinkSessionName != "" {
		label.ResultURI = toolresult.URL(self.resultLinkSessionName, event.ID)
	}

	self.toolBlock.FinaliseRowWithLabel(
		index,
		label,
		getState(event.Status),
		event.Took,
		call.Summary(event),
		call.Measurements(event.Metrics),
	)

	self.attachPicture(index, event)

	if len(self.rows) == 0 {
		self.Close(dynamic.Done)
	}
}

func (self *Picasso) attachPicture(index int, event agent.Event) {
	if event.Picture == nil || self.pictures.sessionDirectory == "" || !self.screen.IsTerminal() {
		return
	}

	drawing, isStored := pictures.Prepare(self.pictures.sessionDirectory, event.Picture)
	if !isStored {
		return
	}

	picture := dynamic.Picture{
		Path:       drawing.Path,
		Width:      drawing.Width,
		Height:     drawing.Height,
		CellWidth:  self.pictures.cellWidth,
		CellHeight: self.pictures.cellHeight,
		IsLocal:    self.pictures.isLocal,
	}

	if !self.pictures.isLocal {
		data, isRead := pictures.Read(drawing.Path)
		if !isRead {
			return
		}
		picture.Data = data
	}

	self.toolBlock.AttachPicture(index, picture)
}

func (self *Picasso) render(event agent.Event) string {
	if event.Kind == agent.StartupEvent {
		return startup.RenderEvent(event, self.screen.Columns(), self.screen.IsTextSizingSupported())
	}

	return NoticeStyle(event.Status)(strutil.CapitaliseSentence(strutil.PrintableLines(event.Text)))
}

func RenderFailure(event agent.Event) string {
	return strutil.CapitaliseSentence(strutil.PrintableLines(event.Text))
}
