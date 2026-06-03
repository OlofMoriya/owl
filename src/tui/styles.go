package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

var (
	primaryColor   = lipgloss.Color("63")
	secondaryColor = lipgloss.Color("240")
	panelColor     = lipgloss.Color("#242424")
	accentColor    = lipgloss.Color("205")
	errorColor     = lipgloss.Color("196")

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(primaryColor).
			MarginBottom(1)

	itemStyle = lipgloss.NewStyle().
			PaddingLeft(2)

	selectedItemStyle = lipgloss.NewStyle().
				PaddingLeft(1).
				Foreground(accentColor).
				Bold(true)

	dimStyle = lipgloss.NewStyle().
			Foreground(secondaryColor)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#a0a0a0")).
			Italic(true).
			MarginTop(1)

	loadingStyle = lipgloss.NewStyle().
			Foreground(primaryColor).
			Bold(true)

	errorStyle = lipgloss.NewStyle().
			Foreground(errorColor).
			Bold(true)

	// Clean, borderless message blocks
	userPromptStyle = lipgloss.NewStyle().
			Padding(1, 2).
			Margin(0, 0, 1, 0).
			Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("#303030"))

	aiResponseStyle = lipgloss.NewStyle().
			Padding(1, 2).
			Margin(0, 0, 1, 0)
		//.
		//Foreground(lipgloss.Color("252"))

	// Set to nil to disable the assistant response background.
	assistantResponseBackgroundColor termenv.Color = termenv.RGBColor("#202020")

	sendingStyle = lipgloss.NewStyle().
			Foreground(primaryColor).
			Bold(true).
			Blink(true)

	// Borderless right-side panel
	usagePanelStyle = lipgloss.NewStyle().
			Padding(1, 2).
			MarginLeft(2)

	usagePanelTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(primaryColor)

	usageMetricLabelStyle = lipgloss.NewStyle().
				Foreground(secondaryColor)

	usageMetricValueStyle = lipgloss.NewStyle().
				Bold(true)
)

// SetAssistantResponseBackgroundColor changes the background used behind live assistant responses.
func SetAssistantResponseBackgroundColor(bg termenv.Color) {
	assistantResponseBackgroundColor = bg
}
