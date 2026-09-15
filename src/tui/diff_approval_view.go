package tui

import (
	"fmt"
	"owl/interaction"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const diffApprovalWideWidth = 110

type diffApprovalPane int

const (
	diffApprovalConversationPane diffApprovalPane = iota
	diffApprovalDiffPane
)

type diffApprovalState struct {
	prompt               interaction.DiffApprovalPrompt
	conversationViewport viewport.Model
	diffViewport         viewport.Model
	reasonInput          textarea.Model
	focusedPane          diffApprovalPane
	capturingReason      bool
	expandedDiff         bool
	previousMode         chatMode
	wide                 bool
	conversationWidth    int
	diffWidth            int
	conversationHeight   int
	diffHeight           int
}

var (
	diffApprovalBorderStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("240"))
	diffApprovalFocusedBorderStyle = diffApprovalBorderStyle.Copy().
					BorderForeground(lipgloss.Color("220"))
	diffApprovalTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("220"))
	diffApprovalAddedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	diffApprovalRemovedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	diffApprovalHeaderStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("44")).Bold(true)
	diffApprovalContextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	diffApprovalRejectStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
)

func (m *chatViewModel) beginDiffApproval(prompt interaction.DiffApprovalPrompt) {
	reasonInput := textarea.New()
	reasonInput.Placeholder = "Explain why the change should be rejected..."
	reasonInput.ShowLineNumbers = false
	reasonInput.SetHeight(3)

	previousMode := m.mode
	if previousMode == chatDiffApprovalMode {
		previousMode = chatInputMode
	}

	m.diffApproval = &diffApprovalState{
		prompt:               prompt,
		reasonInput:          reasonInput,
		focusedPane:          diffApprovalDiffPane,
		previousMode:         previousMode,
		diffViewport:         viewport.New(20, 5),
		conversationViewport: viewport.New(20, 5),
	}
	m.mode = chatDiffApprovalMode
	m.resizeDiffApproval()
	m.diffApproval.conversationViewport.GotoBottom()
	m.diffApproval.diffViewport.GotoTop()
}

func (m *chatViewModel) resizeDiffApproval() {
	state := m.diffApproval
	if state == nil {
		return
	}

	width := max(50, m.width)
	height := max(16, m.height)
	headerRows := 4
	footerRows := 2
	if state.capturingReason {
		footerRows += 5
	}
	bodyHeight := max(8, height-headerRows-footerRows)

	state.wide = width >= diffApprovalWideWidth
	if state.expandedDiff {
		state.conversationWidth = 0
		state.conversationHeight = 0
		state.diffWidth = width
		state.diffHeight = bodyHeight
	} else if state.wide {
		state.conversationWidth = max(38, width*42/100)
		state.diffWidth = max(38, width-state.conversationWidth-1)
		state.conversationHeight = bodyHeight
		state.diffHeight = bodyHeight
	} else {
		state.conversationWidth = width
		state.diffWidth = width
		state.conversationHeight = max(5, bodyHeight/3)
		state.diffHeight = max(6, bodyHeight-state.conversationHeight-1)
	}

	if !state.expandedDiff {
		conversationInnerWidth := max(20, state.conversationWidth-4)
		conversationInnerHeight := max(2, state.conversationHeight-3)
		state.conversationViewport.Width = conversationInnerWidth
		state.conversationViewport.Height = conversationInnerHeight
		state.conversationViewport.SetContent(m.renderConversationContent(conversationInnerWidth))
	}

	diffInnerWidth := max(20, state.diffWidth-4)
	diffInnerHeight := max(2, state.diffHeight-3)
	state.diffViewport.Width = diffInnerWidth
	state.diffViewport.Height = diffInnerHeight
	state.diffViewport.SetContent(styleDiffForApproval(state.prompt.Diff))
	state.reasonInput.SetWidth(max(20, width-4))
}

func (m *chatViewModel) refreshDiffApprovalConversation() {
	state := m.diffApproval
	if state == nil || state.expandedDiff {
		return
	}
	state.conversationViewport.SetContent(m.renderConversationContent(state.conversationViewport.Width))
	state.conversationViewport.GotoBottom()
}

func (m *chatViewModel) handleDiffApprovalKey(msg tea.KeyMsg) tea.Cmd {
	state := m.diffApproval
	if state == nil {
		m.mode = chatInputMode
		return nil
	}

	if state.capturingReason {
		switch msg.String() {
		case "esc":
			state.capturingReason = false
			state.reasonInput.Blur()
			m.resizeDiffApproval()
			return nil
		case "ctrl+w", "ctrl+s":
			reason := strings.TrimSpace(state.reasonInput.Value())
			m.finishDiffApproval(interaction.DiffApprovalResult{
				Decision: interaction.DiffApprovalRejected,
				Reason:   reason,
			})
			return nil
		default:
			var cmd tea.Cmd
			state.reasonInput, cmd = state.reasonInput.Update(msg)
			return cmd
		}
	}

	switch msg.String() {
	case "y", "Y":
		m.finishDiffApproval(interaction.DiffApprovalResult{Decision: interaction.DiffApprovalApproved})
	case "n", "N":
		state.capturingReason = true
		state.reasonInput.Focus()
		m.resizeDiffApproval()
		return textarea.Blink
	case "q", "esc":
		m.finishDiffApproval(interaction.DiffApprovalResult{Decision: interaction.DiffApprovalCancelled})
	case "tab":
		if !state.expandedDiff {
			if state.focusedPane == diffApprovalDiffPane {
				state.focusedPane = diffApprovalConversationPane
			} else {
				state.focusedPane = diffApprovalDiffPane
			}
		}
	case "f":
		state.expandedDiff = !state.expandedDiff
		if state.expandedDiff {
			state.focusedPane = diffApprovalDiffPane
		}
		m.resizeDiffApproval()
	case "j", "down":
		m.activeDiffApprovalViewport().LineDown(1)
	case "k", "up":
		m.activeDiffApprovalViewport().LineUp(1)
	case "d", "ctrl+d":
		m.activeDiffApprovalViewport().HalfViewDown()
	case "u", "ctrl+u":
		m.activeDiffApprovalViewport().HalfViewUp()
	case "pgdown", "ctrl+f":
		m.activeDiffApprovalViewport().PageDown()
	case "pgup", "ctrl+b":
		m.activeDiffApprovalViewport().PageUp()
	case "g", "home":
		m.activeDiffApprovalViewport().GotoTop()
	case "G", "end":
		m.activeDiffApprovalViewport().GotoBottom()
	}

	return nil
}

func (m *chatViewModel) activeDiffApprovalViewport() *viewport.Model {
	if m.diffApproval != nil && m.diffApproval.focusedPane == diffApprovalConversationPane && !m.diffApproval.expandedDiff {
		return &m.diffApproval.conversationViewport
	}
	return &m.diffApproval.diffViewport
}

func (m *chatViewModel) finishDiffApproval(result interaction.DiffApprovalResult) {
	state := m.diffApproval
	if state == nil {
		return
	}

	state.prompt.ResponseChan <- result
	m.mode = state.previousMode
	m.diffApproval = nil
	m.applyLayout()
	m.updateViewportContent()
}

func (m *chatViewModel) renderDiffApprovalWorkspace() string {
	state := m.diffApproval
	if state == nil {
		return ""
	}

	summary := strings.TrimSpace(state.prompt.Summary)
	if summary == "" {
		summary = "No change summary was provided. Review the conversation and diff carefully."
	}

	header := diffApprovalTitleStyle.Render(fmt.Sprintf("Review file update: %s", state.prompt.FileName))
	summaryLine := lipgloss.NewStyle().Foreground(lipgloss.Color("252")).MaxWidth(max(20, m.width)).Render("Purpose: " + summary)

	var body string
	if state.expandedDiff {
		body = renderDiffApprovalPane("Proposed diff [expanded]", state.diffViewport.View(), state.diffWidth, state.diffHeight, true)
	} else {
		conversationFocused := state.focusedPane == diffApprovalConversationPane
		diffFocused := state.focusedPane == diffApprovalDiffPane
		conversation := renderDiffApprovalPane("Conversation / rationale", state.conversationViewport.View(), state.conversationWidth, state.conversationHeight, conversationFocused)
		diff := renderDiffApprovalPane("Proposed diff", state.diffViewport.View(), state.diffWidth, state.diffHeight, diffFocused)
		if state.wide {
			body = lipgloss.JoinHorizontal(lipgloss.Top, conversation, " ", diff)
		} else {
			body = lipgloss.JoinVertical(lipgloss.Left, conversation, diff)
		}
	}

	footerStyle := helpStyle.Copy().Width(max(20, m.width))
	footer := footerStyle.Render("tab: switch pane • j/k or arrows: scroll • d/u: half-page • f: expand diff • y: approve • n: reject • q/esc: cancel")
	if state.capturingReason {
		footer = diffApprovalRejectStyle.Render("Rejection reason") + "\n" + state.reasonInput.View() + "\n" + footerStyle.Render("ctrl+w/ctrl+s: reject • esc: back")
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, summaryLine, body, footer)
}

func renderDiffApprovalPane(title, content string, outerWidth, outerHeight int, focused bool) string {
	innerWidth := max(20, outerWidth-2)
	innerHeight := max(3, outerHeight-2)
	border := diffApprovalBorderStyle
	if focused {
		border = diffApprovalFocusedBorderStyle
		title = "▶ " + title
	} else {
		title = "  " + title
	}
	pane := diffApprovalTitleStyle.Render(title) + "\n" + content
	return border.Copy().Width(innerWidth).Height(innerHeight).Render(pane)
}

func styleDiffForApproval(diff string) string {
	lines := strings.Split(strings.ReplaceAll(diff, "\r\n", "\n"), "\n")
	styled := make([]string, 0, len(lines))
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") || strings.HasPrefix(line, "@@"):
			styled = append(styled, diffApprovalHeaderStyle.Render(line))
		case strings.HasPrefix(line, "+"):
			styled = append(styled, diffApprovalAddedStyle.Render(line))
		case strings.HasPrefix(line, "-"):
			styled = append(styled, diffApprovalRemovedStyle.Render(line))
		default:
			styled = append(styled, diffApprovalContextStyle.Render(line))
		}
	}
	return strings.Join(styled, "\n")
}
