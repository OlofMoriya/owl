package tui

import (
	commontypes "owl/common_types"
	"owl/data"
	"owl/interaction"
	"owl/logger"

	tea "github.com/charmbracelet/bubbletea"
)

type TUIConfig struct {
	Repository   data.HistoryRepository
	Model        commontypes.Model
	HistoryCount int
}

// Run starts the TUI application
func Run(config TUIConfig) error {
	// Create the status channel for logger messages
	logger.StatusChan = make(chan string, 50) // buffered channel
	logger.HistoryPersistedChan = make(chan int64, 50)
	interaction.QuestionPromptChan = make(chan interaction.QuestionPrompt, 10)
	interaction.FileDisplayPromptChan = make(chan interaction.FileDisplayPrompt, 10)
	interaction.DiffApprovalPromptChan = make(chan interaction.DiffApprovalPrompt, 10)
	defer func() {
		// Clean up when TUI exits
		close(logger.StatusChan)
		close(logger.HistoryPersistedChan)
		close(interaction.QuestionPromptChan)
		close(interaction.FileDisplayPromptChan)
		close(interaction.DiffApprovalPromptChan)
		logger.StatusChan = nil
		logger.HistoryPersistedChan = nil
		interaction.QuestionPromptChan = nil
		interaction.FileDisplayPromptChan = nil
		interaction.DiffApprovalPromptChan = nil
	}()

	p := tea.NewProgram(
		initialModel(config),
		tea.WithAltScreen(),
	)

	_, err := p.Run()
	return err
}

func initialModel(config TUIConfig) tea.Model {
	return newListViewModel(config)
}
