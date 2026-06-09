package tools

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// DiffApprovalResult represents the user's decision
type DiffApprovalResult int

const (
	Approved DiffApprovalResult = iota
	Rejected
	Cancelled
)

// DiffViewerModel is the bubbletea model for viewing and approving diffs
type DiffViewerModel struct {
	viewport         viewport.Model
	diff             string
	fileName         string
	ready            bool
	decision         ApprovalDecision
	approved         bool
	quit             bool
	width            int
	height           int
	headerHeight     int
	footerHeight     int
	capturingReason  bool
	reasonInput      textarea.Model
	reasonPromptRows int
}

// Styles for the diff viewer
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#7D56F4")).
			Padding(0, 1)

	addedLineStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00FF00")).
			Bold(true)

	removedLineStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FF0000")).
				Bold(true)

	contextLineStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#888888"))

	headerLineStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00FFFF")).
			Bold(true)

	infoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFF00"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#626262"))

	approveStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00FF00"))

	rejectStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FF0000"))

	cancelStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFF00"))
)

func NewDiffViewerModel(fileName, diff string) DiffViewerModel {
	reasonInput := textarea.New()
	reasonInput.Placeholder = "Enter denial reason..."
	reasonInput.Focus()
	reasonInput.SetHeight(4)
	reasonInput.ShowLineNumbers = false
	reasonInput.Prompt = "> "

	return DiffViewerModel{
		diff:             diff,
		fileName:         fileName,
		headerHeight:     3,
		footerHeight:     3,
		reasonInput:      reasonInput,
		reasonPromptRows: 7,
	}
}

func (m DiffViewerModel) Init() tea.Cmd {
	return nil
}

func (m DiffViewerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.capturingReason {
			switch msg.String() {
			case "esc":
				m.capturingReason = false
				m.reasonInput.Blur()
				return m, nil
			case "ctrl+s":
				m.decision = ApprovalDecision{Result: Rejected, Reason: strings.TrimSpace(m.reasonInput.Value())}
				m.quit = true
				return m, tea.Quit
			}
			m.reasonInput, cmd = m.reasonInput.Update(msg)
			return m, cmd
		}

		switch msg.String() {
		case "q", "esc":
			m.decision = ApprovalDecision{Result: Cancelled}
			m.quit = true
			return m, tea.Quit
		case "y", "Y":
			m.decision = ApprovalDecision{Result: Approved}
			m.approved = true
			m.quit = true
			return m, tea.Quit
		case "n", "N":
			m.capturingReason = true
			m.reasonInput.Focus()
			return m, textarea.Blink
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		viewportHeight := msg.Height - m.headerHeight - m.footerHeight
		if m.capturingReason {
			viewportHeight -= m.reasonPromptRows
			if viewportHeight < 3 {
				viewportHeight = 3
			}
		}

		if !m.ready {
			m.viewport = viewport.New(msg.Width, viewportHeight)
			m.viewport.YPosition = m.headerHeight
			m.viewport.SetContent(m.styleDiff())
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = viewportHeight
		}

		m.reasonInput.SetWidth(max(20, msg.Width-4))
	}

	// Update viewport
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m DiffViewerModel) View() string {
	if !m.ready {
		return "\n  Initializing..."
	}

	// Header
	header := m.headerView()

	// Footer
	footer := m.footerView()

	reasonSection := ""
	if m.capturingReason {
		reasonSection = "\n" + m.reasonView()
	}

	// Combine
	return fmt.Sprintf("%s\n%s%s\n%s", header, m.viewport.View(), reasonSection, footer)
}

func (m DiffViewerModel) headerView() string {
	title := titleStyle.Render(fmt.Sprintf(" 📝 File Update: %s ", m.fileName))
	line := strings.Repeat("─", max(0, m.viewport.Width-lipgloss.Width(title)))
	return lipgloss.JoinHorizontal(lipgloss.Center, title, line)
}

func (m DiffViewerModel) footerView() string {
	// Status line
	info := infoStyle.Render(fmt.Sprintf(" %3.f%%", m.viewport.ScrollPercent()*100))
	line := strings.Repeat("─", max(0, m.viewport.Width-lipgloss.Width(info)))
	scrollInfo := lipgloss.JoinHorizontal(lipgloss.Center, line, info)

	// Help text
	help := helpStyle.Render(" ↑/↓: scroll • ")
	if m.capturingReason {
		help += rejectStyle.Render("ctrl+s: save denial")
		help += helpStyle.Render(" • ")
		help += cancelStyle.Render("esc: back")
	} else {
		help += approveStyle.Render("y: approve")
		help += helpStyle.Render(" • ")
		help += rejectStyle.Render("n: reject")
		help += helpStyle.Render(" • ")
		help += cancelStyle.Render("q: cancel")
	}

	return fmt.Sprintf("%s\n%s", scrollInfo, help)
}

func (m DiffViewerModel) reasonView() string {
	var b strings.Builder
	b.WriteString(rejectStyle.Render("Denial reason required (optional but recommended):"))
	b.WriteString("\n")
	b.WriteString(m.reasonInput.View())
	return b.String()
}

// styleDiff applies syntax highlighting to the diff
func (m DiffViewerModel) styleDiff() string {
	lines := strings.Split(m.diff, "\n")
	var styled strings.Builder

	for _, line := range lines {
		if len(line) == 0 {
			styled.WriteString("\n")
			continue
		}

		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
			styled.WriteString(headerLineStyle.Render(line))
		case strings.HasPrefix(line, "+"):
			styled.WriteString(addedLineStyle.Render(line))
		case strings.HasPrefix(line, "-"):
			styled.WriteString(removedLineStyle.Render(line))
		case strings.HasPrefix(line, "@@"):
			styled.WriteString(infoStyle.Render(line))
		default:
			styled.WriteString(contextLineStyle.Render(line))
		}
		styled.WriteString("\n")
	}

	return styled.String()
}

// ShowDiffForApproval displays the diff and waits for user approval
func ShowDiffForApproval(fileName, diff string) (ApprovalDecision, error) {
	m := NewDiffViewerModel(fileName, diff)
	p := tea.NewProgram(m, tea.WithAltScreen())

	finalModel, err := p.Run()
	if err != nil {
		return ApprovalDecision{Result: Cancelled}, err
	}

	if m, ok := finalModel.(DiffViewerModel); ok {
		return m.decision, nil
	}

	return ApprovalDecision{Result: Cancelled}, fmt.Errorf("unexpected model type")
}
