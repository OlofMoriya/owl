package tui

import (
	"encoding/json"
	"fmt"
	"owl/data"
	"owl/services"
	"owl/tools"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
)

type historyViewMode int

const (
	historyCompactMode historyViewMode = iota
	historyExpandedMode
	historyCodeViewMode
)

type chatHistoryViewModel struct {
	shared       *sharedState
	history      []data.History
	viewport     viewport.Model
	cursor       int
	mode         historyViewMode
	expandedIdx  int // Which message is expanded (-1 = none)
	ready        bool
	loading      bool
	width        int
	height       int
	err          error
	showArchived bool
}

type historyArchivedMsg struct{}

func newChatHistoryViewModel(shared *sharedState) *chatHistoryViewModel {
	vp := viewport.New(shared.width, shared.height-5)
	vp.YPosition = 0

	return &chatHistoryViewModel{
		shared:       shared,
		viewport:     vp,
		cursor:       0,
		mode:         historyCompactMode,
		expandedIdx:  -1,
		loading:      true,
		width:        shared.width,
		height:       shared.height,
		showArchived: true,
	}
}

func (m *chatHistoryViewModel) Init() tea.Cmd {
	return m.loadHistory()
}

func (m *chatHistoryViewModel) loadHistory() tea.Cmd {
	return func() tea.Msg {
		history, err := m.shared.config.Repository.GetHistoryByContextId(
			m.shared.selectedCtx.Id,
			1000, // Load all history for browsing
		)

		if err != nil {
			return errorMsg{err}
		}
		return historyLoadedMsg(history)
	}
}

func (m *chatHistoryViewModel) toggleArchive(historyId int64, currentStatus bool) tea.Cmd {
	return func() tea.Msg {
		err := m.shared.config.Repository.ArchiveHistory(historyId, !currentStatus)
		if err != nil {
			return errorMsg{err}
		}
		return historyArchivedMsg{}
	}
}

func (m *chatHistoryViewModel) scrollToSelection() {
	if len(m.getVisibleHistory()) == 0 {
		return
	}

	// Each message in compact mode takes 3 lines (prompt + response + blank)
	linesPerMessage := 3

	// Calculate the line position of the current cursor
	cursorLine := m.cursor * linesPerMessage

	// Calculate visible range
	viewportTop := m.viewport.YOffset
	viewportBottom := viewportTop + m.viewport.Height

	// If cursor is above viewport, scroll up
	if cursorLine < viewportTop {
		m.viewport.SetYOffset(cursorLine)
	}

	// If cursor is below viewport, scroll down
	// We want the cursor item to be visible, so check if cursor + item height is visible
	cursorBottom := cursorLine + linesPerMessage
	if cursorBottom > viewportBottom {
		// Scroll so the cursor item is at the bottom of the viewport
		newOffset := cursorBottom - m.viewport.Height
		if newOffset < 0 {
			newOffset = 0
		}
		m.viewport.SetYOffset(newOffset)
	}
}

func (m *chatHistoryViewModel) getVisibleHistory() []data.History {
	if m.showArchived {
		return m.history
	}
	var visible []data.History
	for _, h := range m.history {
		if !h.Archived {
			visible = append(visible, h)
		}
	}
	return visible
}

func (m *chatHistoryViewModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var vpCmd tea.Cmd
	visibleHistory := m.getVisibleHistory()

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.loading {
			return m, nil
		}

		// Code view mode
		if m.mode == historyCodeViewMode {
			switch msg.String() {
			case "esc", "q", "c":
				m.mode = historyCompactMode
				m.updateContent()
				return m, nil
			case "y":
				// Copy code to clipboard
				if m.expandedIdx >= 0 && m.expandedIdx < len(visibleHistory) {
					allCode := strings.Join(collectHistoryCodeBlocks(visibleHistory[m.expandedIdx]), "\n\n")
					clipboard.WriteAll(allCode)
				}
				return m, nil
			}
			// Allow scrolling in code view
			m.viewport, vpCmd = m.viewport.Update(msg)
			return m, vpCmd
		}

		// Expanded view mode
		if m.mode == historyExpandedMode {
			switch msg.String() {
			case "esc", "q":
				m.mode = historyCompactMode
				m.expandedIdx = -1
				m.updateContent()
				m.scrollToSelection() // Scroll to cursor when returning to compact
				return m, nil
			case "c":
				// View code
				if m.expandedIdx >= 0 && m.expandedIdx < len(visibleHistory) {
					m.mode = historyCodeViewMode
					m.updateContent()
				}
				return m, nil
			case "y":
				// Copy full response
				if m.expandedIdx >= 0 && m.expandedIdx < len(visibleHistory) {
					clipboard.WriteAll(visibleHistory[m.expandedIdx].Response)
				}
				return m, nil
			case "a":
				// Toggle archive from expanded view
				if m.expandedIdx >= 0 && m.expandedIdx < len(visibleHistory) {
					h := visibleHistory[m.expandedIdx]
					return m, m.toggleArchive(h.Id, h.Archived)
				}
			}
			// Allow scrolling in expanded view
			m.viewport, vpCmd = m.viewport.Update(msg)
			return m, vpCmd
		}

		// Compact mode navigation
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit

		case "esc", "q":
			// Return to chat view
			chatView := newChatViewModel(m.shared)
			return chatView, tea.Sequence(
				chatView.Init(),
				func() tea.Msg {
					return tea.WindowSizeMsg{
						Width:  m.shared.width,
						Height: m.shared.height,
					}
				},
			)

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				m.updateContent()
				m.scrollToSelection()
			}

		case "down", "j":
			if m.cursor < len(visibleHistory)-1 {
				m.cursor++
				m.updateContent()
				m.scrollToSelection()
			}

		case "g":
			// Go to top
			m.cursor = 0
			m.updateContent()
			m.scrollToSelection()

		case "G":
			// Go to bottom
			if len(visibleHistory) > 0 {
				m.cursor = len(visibleHistory) - 1
			}
			m.updateContent()
			m.scrollToSelection()

		case "enter", "e":
			// Expand selected message
			if m.cursor < len(visibleHistory) {
				m.expandedIdx = m.cursor
				m.mode = historyExpandedMode
				m.updateContent()
			}

		case "c":
			// View code for selected message
			if m.cursor < len(visibleHistory) && len(collectHistoryCodeBlocks(visibleHistory[m.cursor])) > 0 {
				m.expandedIdx = m.cursor
				m.mode = historyCodeViewMode
				m.updateContent()
			}

		case "y":
			// Copy response of selected message
			if m.cursor < len(visibleHistory) {
				clipboard.WriteAll(visibleHistory[m.cursor].Response)
			}

		case "a":
			// Toggle archive
			if m.cursor < len(visibleHistory) {
				h := visibleHistory[m.cursor]
				return m, m.toggleArchive(h.Id, h.Archived)
			}

		case "H":
			// Toggle visibility of archived items
			m.showArchived = !m.showArchived
			if m.cursor >= len(m.getVisibleHistory()) && len(m.getVisibleHistory()) > 0 {
				m.cursor = len(m.getVisibleHistory()) - 1
			}
			m.updateContent()
			m.scrollToSelection()

		case "r":
			// Refresh
			m.loading = true
			return m, m.loadHistory()
		}

	case historyLoadedMsg:
		m.history = []data.History(msg)
		m.loading = false
		m.updateContent()
		m.scrollToSelection()

	case historyArchivedMsg:
		return m, m.loadHistory()

	case errorMsg:
		m.err = msg.err
		m.loading = false

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		if !m.ready {
			m.viewport = viewport.New(msg.Width, msg.Height-5)
			m.viewport.YPosition = 0
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - 5
		}
		m.updateContent()
		m.scrollToSelection()
	}

	return m, vpCmd
}

func (m *chatHistoryViewModel) updateContent() {
	visible := m.getVisibleHistory()
	if len(visible) == 0 {
		m.viewport.SetContent(dimStyle.Render("No history matches current filter"))
		return
	}

	switch m.mode {
	case historyCompactMode:
		m.viewport.SetContent(m.renderCompactMode(visible))
	case historyExpandedMode:
		m.viewport.SetContent(m.renderExpandedMode(visible))
	case historyCodeViewMode:
		m.viewport.SetContent(m.renderCodeViewMode(visible))
	}
}

func (m *chatHistoryViewModel) renderCompactMode(visible []data.History) string {
	var b strings.Builder

	for i, h := range visible {
		cursor := "  "
		style := itemStyle

		if i == m.cursor {
			cursor = "▶ "
			style = selectedItemStyle
		}

		// Archived indicator
		archivedPrefix := ""
		if h.Archived {
			archivedPrefix = errorStyle.Render("[ARCHIVED] ")
		}

		// Check if message contains code
		codeIndicator := ""
		if len(collectHistoryCodeBlocks(h)) > 0 {
			codeIndicator = dimStyle.Render(" 💻")
		}

		toolSummary := formatToolCallCompactSummary(h.ToolUse)
		if toolSummary != "" {
			toolSummary = " " + dimStyle.Render("["+toolSummary+"]")
		}

		// Truncate prompt to 1 line
		prompt := h.Prompt
		if len(prompt) > m.width-20 {
			prompt = prompt[:m.width-23] + "..."
		}
		prompt = strings.ReplaceAll(prompt, "\n", " ")

		// Truncate response to 2 lines
		response := h.Response
		lines := strings.Split(response, "\n")
		if len(lines) > 2 {
			response = strings.Join(lines[:2], " ") + "..."
		} else {
			response = strings.ReplaceAll(response, "\n", " ")
		}
		if len(response) > m.width-20 {
			response = response[:m.width-23] + "..."
		}

		// Format: cursor [archived] [#] prompt | response [code]
		line := fmt.Sprintf("%s%s[%d]%s%s %s",
			cursor,
			archivedPrefix,
			i+1,
			codeIndicator,
			toolSummary,
			dimStyle.Render("Q:"))

		msgStyle := style
		if h.Archived {
			msgStyle = dimStyle
		}

		b.WriteString(msgStyle.Render(line))
		b.WriteString(" ")
		b.WriteString(msgStyle.Render(prompt))
		b.WriteString("\n")

		b.WriteString(msgStyle.Render("    " + dimStyle.Render("A: ") + response))
		b.WriteString("\n\n")
	}

	return b.String()
}

func formatToolCallCompactSummary(toolUses []data.ToolUse) string {
	total := len(toolUses)
	if total == 0 {
		return ""
	}

	failed := 0
	for _, tu := range toolUses {
		if !tu.Result.Success {
			failed++
		}
	}
	successful := total - failed

	parts := []string{}
	if successful > 0 {
		parts = append(parts, fmt.Sprintf("%d successful tool %s", successful, pluralizeToolCall(successful)))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed tool %s", failed, pluralizeToolCall(failed)))
	}

	return strings.Join(parts, ", ")
}

func pluralizeToolCall(count int) string {
	if count == 1 {
		return "call"
	}
	return "calls"
}

func (m *chatHistoryViewModel) renderExpandedMode(visible []data.History) string {
	if m.expandedIdx < 0 || m.expandedIdx >= len(visible) {
		return "Invalid message index"
	}

	h := visible[m.expandedIdx]
	var b strings.Builder

	// Header
	archivedStatus := ""
	if h.Archived {
		archivedStatus = errorStyle.Render(" [ARCHIVED]")
	}
	b.WriteString(headerStyle.Render(fmt.Sprintf("Message %d of %d%s", m.expandedIdx+1, len(visible), archivedStatus)))
	b.WriteString("\n\n")

	codeBlocks := collectHistoryCodeBlocks(h)
	if len(codeBlocks) > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("💻 Contains %d code block(s) - press 'c' to view", len(codeBlocks))))
		b.WriteString("\n\n")
	}

	if len(h.ToolUse) > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("🔧 Tool calls: %d", len(h.ToolUse))))
		b.WriteString("\n")
		b.WriteString(renderToolUseHistory(h.ToolUse))
		b.WriteString("\n")
	}

	// Prompt
	b.WriteString(userPromptStyle.Render("Question:"))
	b.WriteString("\n")
	b.WriteString(h.Prompt)
	b.WriteString("\n\n")

	// Response
	b.WriteString(aiResponseStyle.Render("Answer:"))
	b.WriteString("\n")
	rendered, err := glamour.Render(h.Response, "dark")
	if err != nil {
		rendered = h.Response
	}
	b.WriteString(rendered)

	return b.String()
}

func (m *chatHistoryViewModel) renderCodeViewMode(visible []data.History) string {
	if m.expandedIdx < 0 || m.expandedIdx >= len(visible) {
		return "Invalid message index"
	}

	h := visible[m.expandedIdx]
	codeBlocks := collectHistoryCodeBlocks(h)

	var b strings.Builder

	// Header
	b.WriteString(headerStyle.Render(fmt.Sprintf("Code Blocks - Message %d", m.expandedIdx+1)))
	b.WriteString("\n\n")

	if len(codeBlocks) == 0 {
		b.WriteString(dimStyle.Render("No code blocks found"))
	} else {
		for i, block := range codeBlocks {
			b.WriteString(dimStyle.Render(fmt.Sprintf("─── Block %d of %d ───", i+1, len(codeBlocks))))
			b.WriteString("\n")
			b.WriteString(block)
			b.WriteString("\n\n")
		}
	}

	return b.String()
}

func renderToolUseHistory(toolUses []data.ToolUse) string {
	var b strings.Builder
	for idx, tu := range toolUses {
		if idx > 0 {
			b.WriteString("\n")
		}

		lines := tools.FormatToolUseForDisplayRaw(tu)
		for _, line := range lines {
			b.WriteString(styleToolHistoryLine(line))
			b.WriteString("\n")
		}
	}
	return b.String()
}

func styleToolHistoryLine(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return ""
	}

	if strings.HasPrefix(line, "    ") {
		return toolValueStyle.Render(line)
	}

	if strings.Contains(trimmed, " ✗") {
		return toolFailureStyle.Render(trimmed)
	}
	if strings.Contains(trimmed, " ✓") {
		return toolSuccessStyle.Render(trimmed)
	}

	if strings.HasPrefix(trimmed, "warning:") || strings.HasPrefix(trimmed, "warnings:") || strings.HasPrefix(trimmed, "output: withheld") {
		return toolWarningStyle.Render(trimmed)
	}

	if strings.HasPrefix(trimmed, "result:") || strings.HasPrefix(trimmed, "files:") || strings.HasPrefix(trimmed, "failed:") || strings.HasPrefix(trimmed, "output:") {
		return toolResultStyle.Render(trimmed)
	}

	if strings.HasPrefix(trimmed, "code:") {
		return toolResultStyle.Render(trimmed)
	}

	return toolLineStyle.Render(trimmed)
}

func collectHistoryCodeBlocks(h data.History) []string {
	blocks := services.ExtractCodeBlocks(h.Response)
	for _, tu := range h.ToolUse {
		if tu.Name != "run_go_snippet_reviewed" {
			continue
		}
		code := extractReviewedGoSnippetCode(tu.Input)
		if strings.TrimSpace(code) != "" {
			blocks = append(blocks, code)
		}
	}
	return blocks
}

func extractReviewedGoSnippetCode(rawInput string) string {
	if strings.TrimSpace(rawInput) == "" {
		return ""
	}

	var input struct {
		Code string `json:"Code"`
	}
	if err := json.Unmarshal([]byte(rawInput), &input); err != nil {
		return ""
	}

	return input.Code
}

func (m *chatHistoryViewModel) View() string {
	if m.loading {
		return loadingStyle.Render("Loading history...")
	}

	if m.err != nil {
		return errorStyle.Render(fmt.Sprintf("Error: %v\n\nPress ESC to go back", m.err))
	}

	// visible := m.getVisibleHistory()

	// Build help text based on mode
	var help string
	switch m.mode {
	case historyCompactMode:
		help = "↑/↓/j/k navigate • enter/e expand • a archive • H toggle archived • c code • y copy • g top • G bottom • r refresh • esc/q back"
	case historyExpandedMode:
		help = "a archive • c view code • y copy • esc/q back"
	case historyCodeViewMode:
		help = "y copy code • esc/q back"
	}

	modeLabel := ""
	switch m.mode {
	case historyCompactMode:
		archivedCount := 0
		for _, h := range m.history {
			if h.Archived {
				archivedCount++
			}
		}
		modeLabel = fmt.Sprintf("Compact (%d total, %d archived)", len(m.history), archivedCount)
	case historyExpandedMode:
		modeLabel = fmt.Sprintf("Message %d", m.expandedIdx+1)
	case historyCodeViewMode:
		modeLabel = fmt.Sprintf("Code View - Message %d", m.expandedIdx+1)
	}

	return fmt.Sprintf(
		"%s %s\n\n%s\n\n%s",
		headerStyle.Render(fmt.Sprintf("📜 History: %s", m.shared.selectedCtx.Name)),
		dimStyle.Render(fmt.Sprintf("[%s]", modeLabel)),
		m.viewport.View(),
		helpStyle.Render(help),
	)
}
