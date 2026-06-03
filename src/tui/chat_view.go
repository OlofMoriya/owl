package tui

import (
	"context"
	"fmt"
	"os"
	"owl/agents"
	commontypes "owl/common_types"
	"owl/data"
	"owl/interaction"
	"owl/logger"
	"owl/openai_auth"
	picker "owl/picker"
	"owl/services"
	"owl/tools"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	spinner "gabe565.com/spinners"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/ansi"
	"github.com/muesli/reflow/truncate"
	"github.com/muesli/termenv"
)

func renderWithBackground(rendered string, width int, bg termenv.Color) string {
	lines := strings.Split(rendered, "\n")
	reset := "\x1b[0m"
	bgPrefix := ""
	if bg != nil {
		bgPrefix = backgroundPrefix(bg)
	}

	for i, line := range lines {
		// Tabs in markdown/code blocks render wider than rune counts,
		// which can make some lines visually protrude.
		line = strings.ReplaceAll(line, "\t", "    ")

		lineWidth := ansi.PrintableRuneWidth(line)
		if lineWidth > width {
			line = truncate.String(line, uint(width))
			lineWidth = ansi.PrintableRuneWidth(line)
		}

		if lineWidth < width {
			line += strings.Repeat(" ", width-lineWidth)
		}

		if bgPrefix == "" {
			lines[i] = line
			continue
		}

		withBg := bgPrefix + line
		withBg = strings.ReplaceAll(withBg, "\x1b[0m", "\x1b[0m"+bgPrefix)
		withBg = strings.ReplaceAll(withBg, "\x1b[m", "\x1b[m"+bgPrefix)
		lines[i] = withBg + reset
	}

	return strings.Join(lines, "\n")
}

func renderWithBackgroundAndPadding(rendered string, contentWidth int, horizontalPadding int, bg termenv.Color) string {
	if horizontalPadding < 0 {
		horizontalPadding = 0
	}
	pad := strings.Repeat(" ", horizontalPadding)

	lines := strings.Split(rendered, "\n")
	for i, line := range lines {
		lines[i] = pad + line + pad
	}

	return renderWithBackground(strings.Join(lines, "\n"), contentWidth+(2*horizontalPadding), bg)
}

func backgroundPrefix(bg termenv.Color) string {
	sample := termenv.String("X").Background(bg).String()
	idx := strings.Index(sample, "X")
	if idx <= 0 {
		return ""
	}
	return sample[:idx]
}

type chatMode int

type turnType int

const (
	turnTypeUser turnType = iota
	turnTypeInternal
)

type turnState int

const (
	turnStateActive turnState = iota
	turnStateCompleted
	turnStateFailed
	turnStateCanceled
)

type chatTurn struct {
	id                        string
	turnType                  turnType
	state                     turnState
	prompt                    string
	ctx                       context.Context
	cancel                    context.CancelFunc
	responseChan              chan string
	exchangeCompletionChannel chan struct{}
	exchangeEventChannel      chan exchangeEventType
}

type exchangeEventType string

const (
	exchangeEventStepCompleted     exchangeEventType = "step_completed"
	exchangeEventExchangeCompleted exchangeEventType = "exchange_completed"
)

const (
	chatInputMode chatMode = iota
	chatNormalMode
	chatModelSelectMode
	chatQuestionMode
	chatFileDisplayMode
)

type questionAnswerState struct {
	selectedOption  int
	selectedOptions map[int]bool
	customAnswer    string
}

type questionPromptState struct {
	prompt      interaction.QuestionPrompt
	title       string
	questionIdx int
	optionIdx   int
	answers     []questionAnswerState
	customInput textinput.Model
	editingText bool
}

type fileDisplayState struct {
	prompt   interaction.FileDisplayPrompt
	viewport viewport.Model
}

type chatViewModel struct {
	shared                    *sharedState
	history                   []data.History
	textarea                  textarea.Model
	viewport                  viewport.Model
	loading                   bool
	ready                     bool
	width                     int
	height                    int
	err                       error
	historyLoaded             bool
	currentResponse           string
	currentPrompt             string
	responseChan              chan string
	exchangeCompletionChannel chan struct{}
	mode                      chatMode

	// Model selection
	availableModels  []string
	selectedModelIdx int
	modelCursor      int
	availableAgents  []agents.Definition
	selectedAgentIdx int

	historyCount     int
	statusMessage    string
	statusVersion    int
	showUsagePanel   bool
	usagePanelPinned bool
	contextUsage     commontypes.TokenUsage
	lastUsage        *commontypes.TokenUsage
	questionPrompt   *questionPromptState
	fileDisplay      *fileDisplayState
	selectedPDF      string
	selectedSkills   []string
	activeTurns      map[string]*chatTurn
	turnCounter      int64
	spinnerFrames    []string
	spinnerInterval  time.Duration
	spinnerFrame     int
}

const (
	usagePanelMinWidth = 120
	usagePanelWidth    = 32
	minContentWidth    = 40
	minTextareaWidth   = 20
)

type historyLoadedMsg []data.History
type messageReceivedMsg string
type messageDoneMsg struct {
	prompt   string
	response string
}

type statusMsg string
type clearStatusMsg struct {
	version int
}

type spinnerTickMsg struct{}

type historyPersistedMsg int64

type chatChunkMsg struct {
	turnID                    string
	text                      string
	responseChan              chan string
	exchangeCompletionChannel chan struct{}
	exchangeEventChannel      chan exchangeEventType
	prompt                    string
}

type chatExchangeEventMsg struct {
	turnID                    string
	eventType                 exchangeEventType
	responseChan              chan string
	exchangeCompletionChannel chan struct{}
	exchangeEventChannel      chan exchangeEventType
	prompt                    string
}

type chatCompleteMsg struct {
	turnID   string
	prompt   string
	response string
}

type chatErrorMsg struct {
	turnID string
	err    error
}

type questionPromptMsg struct {
	prompt interaction.QuestionPrompt
}

type fileDisplayPromptMsg struct {
	prompt interaction.FileDisplayPrompt
}

type authCommandResultMsg struct {
	text string
	err  error
}

func newChatViewModel(shared *sharedState) *chatViewModel {
	ta := textarea.New()
	ta.Placeholder = "Type your message..."
	ta.Focus()
	ta.CharLimit = 5000
	ta.SetWidth(shared.width - 4)
	ta.SetHeight(3)

	vp := viewport.New(shared.width, shared.height-10)
	vp.YPosition = 0

	availableModels := []string{
		"sonnet",
		"codex",
		"codex-chat",
		"grok",
		"opus",
		"gpt",
		"gpt-chat",
		"haiku",
		"ollama",
		"claude",
	}

	availableAgents := agents.List()
	selectedAgentIdx := 0
	selectedModelIdx := 0
	preferredAgent := strings.TrimSpace(shared.selectedCtx.PreferredAgent)
	if preferredAgent == "" {
		preferredAgent = "planner"
	}
	for idx, agent := range availableAgents {
		if agent.Name == preferredAgent {
			selectedAgentIdx = idx
			break
		}
	}
	preferredModel := strings.TrimSpace(shared.selectedCtx.PreferredModel)
	if preferredModel != "" {
		for idx, model := range availableModels {
			if model == preferredModel {
				selectedModelIdx = idx
				break
			}
		}
	}
	if openai_auth.HasCodexOAuthCredential() {
		if preferredModel == "" {
			for idx, model := range availableModels {
				if model == "gpt" {
					selectedModelIdx = idx
					break
				}
			}
		}
	}
	selectedSkills := parseCSV(shared.selectedCtx.PreferredSkills)

	m := &chatViewModel{
		shared:           shared,
		textarea:         ta,
		viewport:         vp,
		loading:          true,
		ready:            false,
		width:            shared.width,
		height:           shared.height,
		availableModels:  availableModels,
		selectedModelIdx: selectedModelIdx,
		availableAgents:  availableAgents,
		selectedAgentIdx: selectedAgentIdx,
		mode:             chatInputMode,
		historyCount:     shared.config.HistoryCount,
		statusMessage:    "",
		showUsagePanel:   shared.width >= usagePanelMinWidth,
		selectedSkills:   selectedSkills,
		activeTurns:      map[string]*chatTurn{},
		spinnerFrames:    spinner.Dots.Frames,
		spinnerInterval:  spinner.Dots.Interval,
	}
	m.applyLayout()
	return m
}

func (m *chatViewModel) Init() tea.Cmd {
	logger.Debug.Printf("init")

	return tea.Batch(
		textarea.Blink,
		m.loadHistory(),
		m.listenForStatus(),
		m.listenForHistoryPersisted(),
		m.listenForQuestionPrompts(),
		m.listenForFileDisplayPrompts(),
	)
}

func (m *chatViewModel) listenForStatus() tea.Cmd {
	return func() tea.Msg {
		if logger.StatusChan == nil {
			return nil
		}

		msg, ok := <-logger.StatusChan
		if !ok {
			return nil
		}

		return statusMsg(msg)
	}
}

func (m *chatViewModel) spinnerTickCmd() tea.Cmd {
	interval := m.spinnerInterval
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}
	return tea.Tick(interval, func(time.Time) tea.Msg {
		return spinnerTickMsg{}
	})
}

func (m *chatViewModel) listenForHistoryPersisted() tea.Cmd {
	return func() tea.Msg {
		if logger.HistoryPersistedChan == nil {
			return nil
		}

		contextId, ok := <-logger.HistoryPersistedChan
		if !ok {
			return nil
		}

		return historyPersistedMsg(contextId)
	}
}

func (m *chatViewModel) listenForQuestionPrompts() tea.Cmd {
	return func() tea.Msg {
		if interaction.QuestionPromptChan == nil {
			return nil
		}

		prompt, ok := <-interaction.QuestionPromptChan
		if !ok {
			return nil
		}

		return questionPromptMsg{prompt: prompt}
	}
}

func (m *chatViewModel) listenForFileDisplayPrompts() tea.Cmd {
	return func() tea.Msg {
		if interaction.FileDisplayPromptChan == nil {
			return nil
		}

		prompt, ok := <-interaction.FileDisplayPromptChan
		if !ok {
			return nil
		}

		return fileDisplayPromptMsg{prompt: prompt}
	}
}

func (m *chatViewModel) loadHistory() tea.Cmd {
	return func() tea.Msg {
		history, err := m.shared.config.Repository.GetHistoryByContextId(
			m.shared.selectedCtx.Id,
			1000,
		)

		if err != nil {
			return errorMsg{err}
		}
		logger.Debug.Println("returning historyLoadedMsg")
		return historyLoadedMsg(history)
	}
}

func (m *chatViewModel) sendMessage(prompt string) tea.Cmd {
	return func() tea.Msg {
		responseChan := make(chan string, 100)
		exchangeCompletionChannel := make(chan struct{})
		exchangeEventChannel := make(chan exchangeEventType, 8)
		ctx, cancel := context.WithCancel(context.Background())
		turnID := m.nextTurnID(turnTypeUser)
		m.registerTurn(&chatTurn{
			id:                        turnID,
			turnType:                  turnTypeUser,
			state:                     turnStateActive,
			prompt:                    prompt,
			ctx:                       ctx,
			cancel:                    cancel,
			responseChan:              responseChan,
			exchangeCompletionChannel: exchangeCompletionChannel,
			exchangeEventChannel:      exchangeEventChannel,
		})

		handler := &tuiResponseHandler{
			responseChan:              responseChan,
			exchangeCompletionChannel: exchangeCompletionChannel,
			exchangeEventChannel:      exchangeEventChannel,
			fullResponse:              "",
			Repository:                m.shared.config.Repository,
		}

		modelName := m.availableModels[m.selectedModelIdx]

		model, actualModelName := picker.GetModelForQuery(
			modelName,
			m.shared.selectedCtx,
			handler,
			m.shared.config.Repository,
			true,
			true,
			false,
			false,
		)

		m.shared.config.Model = model

		logger.Debug.Printf("MODEL SELECTION: %s (actual: %s)", modelName, actualModelName)

		go func() {
			defer func() {
				if r := recover(); r != nil {
					errText := fmt.Sprintf("Error: %v", r)
					logger.Debug.Printf("recovered panic from streamed query: %v", r)
					handler.RecievedText("\n"+errText+"\n", nil)
					handler.FinalText(m.shared.selectedCtx.Id, prompt, handler.fullResponse+"\n"+errText+"\n", nil, actualModelName, nil)
				}
			}()

			select {
			case <-ctx.Done():
				return
			default:
			}

			selectedAgent := m.currentAgent()
			contextForRequest := *m.shared.selectedCtx
			skillsPrompt := loadSkillsPromptByNames(m.selectedSkills)
			contextForRequest.SystemPrompt = composeAgentSystemPrompt(contextForRequest.SystemPrompt, selectedAgent)
			if strings.TrimSpace(skillsPrompt) != "" {
				contextForRequest.SystemPrompt = composePromptSections(contextForRequest.SystemPrompt, skillsPrompt)
			}

			services.StreamedQuery(
				prompt,
				model,
				m.shared.config.Repository,
				m.historyCount,
				&contextForRequest,
				&commontypes.PayloadModifiers{
					Pdf:              m.selectedPDF,
					Web:              false,
					Image:            false,
					ToolGroupFilters: tools.ToolGroupsToStrings(selectedAgent.ToolGroups),
				},
				actualModelName,
			)
		}()

		waitCmd := waitForChatActivity(turnID, responseChan, exchangeCompletionChannel, exchangeEventChannel, prompt)
		return waitCmd()
	}
}

func waitForChatActivity(turnID string, responseChan chan string, exchangeCompletionChannel chan struct{}, exchangeEventChannel chan exchangeEventType,
	prompt string) tea.Cmd {

	logger.Debug.Println("waitForChatActivity started")

	return func() tea.Msg {
		select {
		case text, ok := <-responseChan:
			// logger.Debug.Printf("responseChan: %v: %s", ok, text)
			if !ok {
				return chatCompleteMsg{turnID: turnID, prompt: prompt}
			}
			return chatChunkMsg{
				turnID:                    turnID,
				text:                      text,
				responseChan:              responseChan,
				exchangeCompletionChannel: exchangeCompletionChannel,
				exchangeEventChannel:      exchangeEventChannel,
				prompt:                    prompt,
			}
		case eventType := <-exchangeEventChannel:
			return chatExchangeEventMsg{
				turnID:                    turnID,
				eventType:                 eventType,
				responseChan:              responseChan,
				exchangeCompletionChannel: exchangeCompletionChannel,
				exchangeEventChannel:      exchangeEventChannel,
				prompt:                    prompt,
			}
		case <-exchangeCompletionChannel:
			logger.Debug.Printf("exchangeCompletionChannel from waitForChatActivity")
			return chatCompleteMsg{turnID: turnID, prompt: prompt}
		}
	}
}

func (m *chatViewModel) nextTurnID(tt turnType) string {
	m.turnCounter++
	prefix := "user"
	if tt == turnTypeInternal {
		prefix = "internal"
	}
	return fmt.Sprintf("%s-%d", prefix, m.turnCounter)
}

func (m *chatViewModel) registerTurn(turn *chatTurn) {
	if turn == nil {
		return
	}
	if m.activeTurns == nil {
		m.activeTurns = map[string]*chatTurn{}
	}
	m.activeTurns[turn.id] = turn
}

func (m *chatViewModel) getTurn(turnID string) (*chatTurn, bool) {
	if m.activeTurns == nil {
		return nil, false
	}
	turn, ok := m.activeTurns[turnID]
	return turn, ok
}

func (m *chatViewModel) completeTurn(turnID string, state turnState) {
	if m.activeTurns == nil {
		return
	}
	turn, ok := m.activeTurns[turnID]
	if !ok {
		return
	}
	if turn.cancel != nil && turn.state == turnStateActive {
		turn.cancel()
		turn.cancel = nil
	}
	turn.state = state
}

func (m *chatViewModel) cancelActiveUserTurns() {
	if m.activeTurns == nil {
		return
	}
	for id, turn := range m.activeTurns {
		if turn != nil && turn.turnType == turnTypeUser && turn.state == turnStateActive {
			m.completeTurn(id, turnStateCanceled)
		}
	}
}

func (m *chatViewModel) hasActiveUserTurn() bool {
	for _, turn := range m.activeTurns {
		if turn != nil && turn.turnType == turnTypeUser && turn.state == turnStateActive {
			return true
		}
	}
	return false
}

func (m *chatViewModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		tiCmd tea.Cmd
		vpCmd tea.Cmd
	)

	shouldUpdateViewport := false
	customViewportHandled := false

	switch msg := msg.(type) {
	case statusMsg:
		cleaned := strings.TrimSpace(string(msg))
		cleaned = strings.ReplaceAll(cleaned, "\n", " ")
		cleaned = strings.Join(strings.Fields(cleaned), " ")

		if cleaned == "" {
			m.statusMessage = ""
			return m, m.listenForStatus()
		}

		m.statusMessage = cleaned
		m.statusVersion++
		currentVersion := m.statusVersion
		logger.Debug.Printf("Received status: %s", cleaned)

		return m, tea.Batch(
			m.listenForStatus(),
			m.clearStatusAfterDelay(currentVersion),
		)

	case clearStatusMsg:
		if msg.version == m.statusVersion {
			m.statusMessage = ""
		}
		return m, nil

	case spinnerTickMsg:
		if !m.hasActiveUserTurn() {
			m.spinnerFrame = 0
			return m, nil
		}
		if len(m.spinnerFrames) > 0 {
			m.spinnerFrame = (m.spinnerFrame + 1) % len(m.spinnerFrames)
		}
		return m, m.spinnerTickCmd()

	case historyPersistedMsg:
		if int64(msg) == m.shared.selectedCtx.Id {
			m.currentResponse = ""
			return m, tea.Batch(
				m.listenForHistoryPersisted(),
				m.loadHistory(),
			)
		}
		return m, m.listenForHistoryPersisted()

	case chatChunkMsg:
		if _, ok := m.getTurn(msg.turnID); !ok {
			return m, nil
		}
		m.currentResponse += msg.text
		m.updateViewportContent()
		return m, waitForChatActivity(msg.turnID, msg.responseChan, msg.exchangeCompletionChannel, msg.exchangeEventChannel, msg.prompt)

	case chatExchangeEventMsg:
		if _, ok := m.getTurn(msg.turnID); !ok {
			return m, nil
		}
		if msg.eventType == exchangeEventExchangeCompleted {
			m.completeTurn(msg.turnID, turnStateCompleted)
			logger.Debug.Println("got exchange completed event")
			m.loading = false
			m.statusMessage = ""
			return m, m.loadHistory()
		}
		return m, waitForChatActivity(msg.turnID, msg.responseChan, msg.exchangeCompletionChannel, msg.exchangeEventChannel, msg.prompt)

	case chatCompleteMsg:
		if _, ok := m.getTurn(msg.turnID); !ok {
			return m, nil
		}
		m.completeTurn(msg.turnID, turnStateCompleted)
		logger.Debug.Println("got chatCompleteMsg")
		m.loading = false
		m.statusMessage = ""
		return m, m.loadHistory()

	case chatErrorMsg:
		if _, ok := m.getTurn(msg.turnID); ok {
			m.completeTurn(msg.turnID, turnStateFailed)
		}
		m.err = msg.err
		m.loading = false
		m.statusMessage = ""
		return m, nil

	case authCommandResultMsg:
		if msg.err != nil {
			m.statusMessage = msg.err.Error()
		} else {
			m.statusMessage = msg.text
		}
		m.statusVersion++
		return m, m.clearStatusAfterDelay(m.statusVersion)

	case questionPromptMsg:
		state := newQuestionPromptState(msg.prompt)
		m.questionPrompt = &state
		m.mode = chatQuestionMode
		return m, m.listenForQuestionPrompts()

	case fileDisplayPromptMsg:
		state := newFileDisplayState(msg.prompt, m.contentWidth(), m.height)
		m.fileDisplay = &state
		m.mode = chatFileDisplayMode
		return m, m.listenForFileDisplayPrompts()

	case tea.KeyMsg:
		if m.mode == chatQuestionMode {
			return m, m.handleQuestionModeKey(msg)
		}
		if m.mode == chatFileDisplayMode {
			return m, m.handleFileDisplayModeKey(msg)
		}

		if m.mode == chatNormalMode {
			switch msg.String() {
			case "ctrl+x":
				m.cancelActiveUserTurns()
				m.statusMessage = "Canceled active turn"
				m.statusVersion++
				return m, m.clearStatusAfterDelay(m.statusVersion)

			case "i":
				m.mode = chatInputMode
				m.textarea.Focus()
				return m, nil

			case "esc", "q":
				listView := newListViewModel(m.shared.config)
				return listView, tea.Sequence(
					listView.Init(),
					func() tea.Msg {
						return tea.WindowSizeMsg{
							Width:  m.shared.width,
							Height: m.shared.height,
						}
					},
				)

			case "=", "+":
				if m.historyCount < services.DefaultHistoryCount {
					m.historyCount++
					return m, m.loadHistory()
				}
				return m, nil

			case "-", "_":
				if m.historyCount > 1 {
					m.historyCount--
					return m, m.loadHistory()
				}
				return m, nil

			case "d", "ctrl+d":
				m.scrollHalfPage(true)
				customViewportHandled = true

			case "u", "ctrl+u":
				m.scrollHalfPage(false)
				customViewportHandled = true

			case "f", "ctrl+f", "pgdown":
				shouldUpdateViewport = true
				m.viewport.PageDown()

			case "b", "ctrl+b", "pgup":
				shouldUpdateViewport = true
				m.viewport.PageUp()

			case "g":
				shouldUpdateViewport = true
				m.viewport.GotoTop()

			case "G":
				shouldUpdateViewport = true
				m.viewport.GotoBottom()

			case "j", "down":
				shouldUpdateViewport = true
				m.viewport.ScrollDown(1)

			case "k", "up":
				shouldUpdateViewport = true
				m.viewport.ScrollUp(1)

			case "ctrl+a":
				historyView := newChatHistoryViewModel(m.shared)
				return historyView, tea.Sequence(
					historyView.Init(),
					func() tea.Msg {
						return tea.WindowSizeMsg{
							Width:  m.shared.width,
							Height: m.shared.height,
						}
					},
				)

			case "ctrl+g":
				m.mode = chatModelSelectMode
				m.modelCursor = m.selectedModelIdx
				return m, nil

			case "ctrl+s":
				cmd := fmt.Sprintf("owl --context_name %s --prompt \"\"",
					m.shared.selectedCtx.Name)
				clipboard.WriteAll(cmd)
				return m, nil

			case "ctrl+t":
				m.usagePanelPinned = true
				m.showUsagePanel = !m.showUsagePanel
				m.applyLayout()
				m.updateViewportContent()
				return m, nil
			}

			if shouldUpdateViewport {
				m.viewport, vpCmd = m.viewport.Update(msg)
			}
			return m, vpCmd
		}

		if m.mode == chatModelSelectMode {
			switch msg.String() {
			case "esc", "q":
				m.mode = chatInputMode
				m.modelCursor = m.selectedModelIdx
				return m, nil

			case "up", "k":
				if m.modelCursor > 0 {
					m.modelCursor--
				}

			case "down", "j":
				if m.modelCursor < len(m.availableModels)-1 {
					m.modelCursor++
				}

			case "enter":
				m.selectedModelIdx = m.modelCursor
				m.mode = chatInputMode
				if m.shared.selectedCtx != nil {
					model := m.availableModels[m.selectedModelIdx]
					_ = m.shared.config.Repository.UpdatePreferredModel(m.shared.selectedCtx.Id, model)
					m.shared.selectedCtx.PreferredModel = model
				}
				return m, nil
			}
			return m, nil
		}

		switch msg.String() {
		case "ctrl+x":
			m.cancelActiveUserTurns()
			m.statusMessage = "Canceled active turn"
			m.statusVersion++
			return m, m.clearStatusAfterDelay(m.statusVersion)

		case "tab":
			m.selectNextAgent()
			m.persistAgentPreference()
			return m, nil

		case "shift+tab":
			m.selectPreviousAgent()
			m.persistAgentPreference()
			return m, nil

		case "ctrl+n":
			m.mode = chatNormalMode
			m.textarea.Blur()
			return m, nil

		case "ctrl+a":
			historyView := newChatHistoryViewModel(m.shared)
			return historyView, tea.Sequence(
				historyView.Init(),
				func() tea.Msg {
					return tea.WindowSizeMsg{
						Width:  m.shared.width,
						Height: m.shared.height,
					}
				},
			)

		case "ctrl+c":
			return m, tea.Quit

		case "esc":
			listView := newListViewModel(m.shared.config)
			return listView, tea.Sequence(
				listView.Init(),
				func() tea.Msg {
					return tea.WindowSizeMsg{
						Width:  m.shared.width,
						Height: m.shared.height,
					}
				},
			)

		case "ctrl+s":
			cmd := fmt.Sprintf("owl --context_name %s --prompt \"\"",
				m.shared.selectedCtx.Name)
			clipboard.WriteAll(cmd)

		case "ctrl+g":
			m.mode = chatModelSelectMode
			m.modelCursor = m.selectedModelIdx
			return m, nil

		case "ctrl+t":
			m.usagePanelPinned = true
			m.showUsagePanel = !m.showUsagePanel
			m.applyLayout()
			m.updateViewportContent()
			return m, nil

		case "ctrl+w":
			if !m.hasActiveUserTurn() && m.textarea.Value() != "" {
				prompt := m.textarea.Value()
				if strings.HasPrefix(strings.TrimSpace(prompt), "/") {
					m.textarea.Reset()
					return m, m.handleSlashCommand(prompt)
				}
				m.currentPrompt = prompt
				m.currentResponse = ""
				m.spinnerFrame = 0
				m.textarea.Reset()
				return m, tea.Batch(m.sendMessage(prompt), m.spinnerTickCmd())
			}
			if m.hasActiveUserTurn() {
				m.statusMessage = "Wait for active turn to complete"
				m.statusVersion++
				return m, m.clearStatusAfterDelay(m.statusVersion)
			}

		case "ctrl+u":
			m.scrollHalfPage(false)
			customViewportHandled = true

		case "ctrl+d":
			m.scrollHalfPage(true)
			customViewportHandled = true

		case "ctrl+b", "pgup":
			shouldUpdateViewport = true
			m.viewport.PageUp()

		case "ctrl+f", "pgdown":
			shouldUpdateViewport = true
			m.viewport.PageDown()

		default:
			m.textarea, tiCmd = m.textarea.Update(msg)
			return m, tiCmd
		}

	case historyLoadedMsg:
		logger.Debug.Printf("historyLoaded msg handled")
		m.history = []data.History(msg)
		m.loading = false
		m.historyLoaded = true
		m.recalculateUsage()
		m.updateViewportContent()

	case messageDoneMsg:
		return m, m.loadHistory()

	case errorMsg:
		m.err = msg.err
		m.loading = false

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		logger.Debug.Printf("windowSizeMsg")
		if !m.usagePanelPinned {
			m.showUsagePanel = msg.Width >= usagePanelMinWidth
		}

		if !m.ready {
			logger.Debug.Printf("windowSizeMsg not ready")
			m.viewport = viewport.New(m.contentWidth(), msg.Height-10)
			m.viewport.YPosition = 0
			m.ready = true
		}

		m.applyLayout()
		m.updateViewportContent()
		if m.fileDisplay != nil {
			m.fileDisplay.viewport.Width = m.contentWidth()
			m.fileDisplay.viewport.Height = max(5, m.height-8)
		}
	}

	if shouldUpdateViewport && !customViewportHandled {
		m.viewport, vpCmd = m.viewport.Update(msg)
	}

	return m, tea.Batch(tiCmd, vpCmd)
}

func (m *chatViewModel) clearStatusAfterDelay(version int) tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return clearStatusMsg{version: version}
	})
}

func (m *chatViewModel) handleSlashCommand(raw string) tea.Cmd {
	trimmed := strings.TrimSpace(raw)
	parts := strings.Fields(trimmed)
	if len(parts) == 0 {
		return func() tea.Msg {
			return authCommandResultMsg{err: fmt.Errorf("empty command")}
		}
	}

	if parts[0] == "/pdf" {
		return m.handlePDFSlashCommand(parts)
	}

	if parts[0] == "/skills" {
		return m.handleSkillsSlashCommand(parts)
	}

	if parts[0] != "/auth" {
		return func() tea.Msg {
			return authCommandResultMsg{err: fmt.Errorf("unsupported command: %s", parts[0])}
		}
	}

	if len(parts) < 3 || strings.ToLower(parts[1]) != "openai" {
		return func() tea.Msg {
			return authCommandResultMsg{err: fmt.Errorf("usage: /auth openai <login|status|logout>")}
		}
	}

	action := strings.ToLower(parts[2])
	return func() tea.Msg {
		switch action {
		case "status":
			return authCommandResultMsg{text: fmt.Sprintf("openai auth status: %s", openai_auth.CurrentStatus())}
		case "logout":
			if err := openai_auth.Logout(); err != nil {
				return authCommandResultMsg{err: fmt.Errorf("openai logout failed: %w", err)}
			}
			return authCommandResultMsg{text: "OpenAI auth cleared."}
		case "login":
			msg, err := openai_auth.Login()
			if err != nil {
				return authCommandResultMsg{err: fmt.Errorf("openai login failed: %w", err)}
			}
			return authCommandResultMsg{text: msg}
		default:
			return authCommandResultMsg{err: fmt.Errorf("unsupported auth action: %s", action)}
		}
	}
}

func (m *chatViewModel) handlePDFSlashCommand(parts []string) tea.Cmd {
	if len(parts) < 2 {
		return func() tea.Msg { return authCommandResultMsg{err: fmt.Errorf("usage: /pdf <set|clear|show> [path]")} }
	}
	action := strings.ToLower(parts[1])

	switch action {
	case "show":
		value := "(none)"
		if strings.TrimSpace(m.selectedPDF) != "" {
			value = m.selectedPDF
		}
		return func() tea.Msg { return authCommandResultMsg{text: fmt.Sprintf("pdf: %s", value)} }
	case "clear":
		m.selectedPDF = ""
		return func() tea.Msg { return authCommandResultMsg{text: "pdf cleared"} }
	case "set":
		if len(parts) < 3 {
			return func() tea.Msg { return authCommandResultMsg{err: fmt.Errorf("usage: /pdf set <path>")} }
		}
		path := strings.TrimSpace(strings.Join(parts[2:], " "))
		if !strings.HasSuffix(strings.ToLower(path), ".pdf") {
			return func() tea.Msg { return authCommandResultMsg{err: fmt.Errorf("pdf path must end with .pdf")} }
		}
		if _, err := os.Stat(path); err != nil {
			return func() tea.Msg { return authCommandResultMsg{err: fmt.Errorf("pdf not found: %s", path)} }
		}
		m.selectedPDF = path
		return func() tea.Msg { return authCommandResultMsg{text: fmt.Sprintf("pdf set: %s", path)} }
	default:
		return func() tea.Msg { return authCommandResultMsg{err: fmt.Errorf("usage: /pdf <set|clear|show> [path]")} }
	}
}

func (m *chatViewModel) handleSkillsSlashCommand(parts []string) tea.Cmd {
	if len(parts) < 2 {
		return func() tea.Msg {
			return authCommandResultMsg{err: fmt.Errorf("usage: /skills <show|list|set|clear> [skill1,skill2]")}
		}
	}
	action := strings.ToLower(parts[1])

	switch action {
	case "show":
		if len(m.selectedSkills) == 0 {
			return func() tea.Msg { return authCommandResultMsg{text: "skills: (none)"} }
		}
		return func() tea.Msg {
			return authCommandResultMsg{text: fmt.Sprintf("skills: %s", strings.Join(m.selectedSkills, ", "))}
		}
	case "list":
		skills, err := availableSkillNames()
		if err != nil {
			return func() tea.Msg { return authCommandResultMsg{err: err} }
		}
		if len(skills) == 0 {
			return func() tea.Msg { return authCommandResultMsg{text: "available skills: (none)"} }
		}
		return func() tea.Msg {
			return authCommandResultMsg{text: fmt.Sprintf("available skills: %s", strings.Join(skills, ", "))}
		}
	case "clear":
		m.selectedSkills = nil
		m.persistSkillsPreference()
		return func() tea.Msg { return authCommandResultMsg{text: "skills cleared"} }
	case "set":
		if len(parts) < 3 {
			return func() tea.Msg { return authCommandResultMsg{err: fmt.Errorf("usage: /skills set <skill1,skill2>")} }
		}
		raw := strings.Join(parts[2:], " ")
		names := parseCSV(raw)
		missing := missingSkills(names)
		if len(missing) > 0 {
			return func() tea.Msg {
				return authCommandResultMsg{err: fmt.Errorf("unknown skills: %s", strings.Join(missing, ", "))}
			}
		}
		m.selectedSkills = names
		m.persistSkillsPreference()
		if len(names) == 0 {
			return func() tea.Msg { return authCommandResultMsg{text: "skills cleared"} }
		}
		return func() tea.Msg {
			return authCommandResultMsg{text: fmt.Sprintf("skills set: %s", strings.Join(names, ", "))}
		}
	default:
		return func() tea.Msg {
			return authCommandResultMsg{err: fmt.Errorf("usage: /skills <show|list|set|clear> [skill1,skill2]")}
		}
	}
}

func newQuestionPromptState(prompt interaction.QuestionPrompt) questionPromptState {
	title := strings.TrimSpace(prompt.Request.Title)
	if title == "" {
		title = "Answer Questions"
	}

	answers := make([]questionAnswerState, len(prompt.Request.Questions))
	for i := range answers {
		answers[i] = questionAnswerState{selectedOption: -1, selectedOptions: map[int]bool{}}
	}

	ti := textinput.New()
	ti.Placeholder = "Type custom answer"
	ti.Prompt = "> "
	ti.CharLimit = 400

	return questionPromptState{
		prompt:      prompt,
		title:       title,
		questionIdx: 0,
		optionIdx:   0,
		answers:     answers,
		customInput: ti,
		editingText: false,
	}
}

func newFileDisplayState(prompt interaction.FileDisplayPrompt, width int, height int) fileDisplayState {
	vp := viewport.New(max(20, width), max(5, height-8))
	vp.YPosition = 0
	vp.SetContent(prompt.Content)
	return fileDisplayState{prompt: prompt, viewport: vp}
}

func (m *chatViewModel) handleQuestionModeKey(msg tea.KeyMsg) tea.Cmd {
	if m.questionPrompt == nil {
		m.mode = chatInputMode
		return nil
	}

	qs := m.questionPrompt
	question := qs.prompt.Request.Questions[qs.questionIdx]

	if qs.editingText {
		switch msg.String() {
		case "esc":
			qs.editingText = false
			qs.customInput.Blur()
			return nil
		case "enter":
			qs.answers[qs.questionIdx].customAnswer = strings.TrimSpace(qs.customInput.Value())
			qs.editingText = false
			qs.customInput.Blur()
			return nil
		case "ctrl+w":
			qs.answers[qs.questionIdx].customAnswer = strings.TrimSpace(qs.customInput.Value())
			qs.editingText = false
			qs.customInput.Blur()
			response, err := buildQuestionBatchResponse(qs)
			if err != nil {
				m.statusMessage = err.Error()
				m.statusVersion++
				return m.clearStatusAfterDelay(m.statusVersion)
			}
			qs.prompt.ResponseChan <- interaction.QuestionPromptResult{Response: response}
			m.questionPrompt = nil
			m.mode = chatInputMode
			return nil
		default:
			var cmd tea.Cmd
			qs.customInput, cmd = qs.customInput.Update(msg)
			return cmd
		}
	}

	switch msg.String() {
	case "j", "down":
		if qs.optionIdx < len(question.Options)-1 {
			qs.optionIdx++
		}
	case "k", "up":
		if qs.optionIdx > 0 {
			qs.optionIdx--
		}
	case "tab", "l", "right":
		if qs.questionIdx < len(qs.prompt.Request.Questions)-1 {
			qs.questionIdx++
			qs.optionIdx = 0
		}
	case "shift+tab", "h", "left":
		if qs.questionIdx > 0 {
			qs.questionIdx--
			qs.optionIdx = 0
		}
	case "enter":
		if len(question.Options) == 0 && question.AllowCustom {
			qs.editingText = true
			qs.customInput.SetValue(qs.answers[qs.questionIdx].customAnswer)
			qs.customInput.Focus()
			return nil
		}
		if len(question.Options) > 0 {
			if question.AllowMultiple {
				if qs.answers[qs.questionIdx].selectedOptions == nil {
					qs.answers[qs.questionIdx].selectedOptions = map[int]bool{}
				}
				qs.answers[qs.questionIdx].selectedOptions[qs.optionIdx] = !qs.answers[qs.questionIdx].selectedOptions[qs.optionIdx]
			} else {
				qs.answers[qs.questionIdx].selectedOption = qs.optionIdx
			}
		}
	case " ":
		if question.AllowMultiple && len(question.Options) > 0 {
			if qs.answers[qs.questionIdx].selectedOptions == nil {
				qs.answers[qs.questionIdx].selectedOptions = map[int]bool{}
			}
			qs.answers[qs.questionIdx].selectedOptions[qs.optionIdx] = !qs.answers[qs.questionIdx].selectedOptions[qs.optionIdx]
		}
	case "i":
		if question.AllowCustom {
			qs.editingText = true
			qs.customInput.SetValue(qs.answers[qs.questionIdx].customAnswer)
			qs.customInput.Focus()
		}
	case "x":
		qs.answers[qs.questionIdx] = questionAnswerState{selectedOption: -1, selectedOptions: map[int]bool{}}
	case "esc":
		qs.prompt.ResponseChan <- interaction.QuestionPromptResult{Err: fmt.Errorf("questionnaire canceled")}
		m.questionPrompt = nil
		m.mode = chatInputMode
	case "ctrl+w":
		response, err := buildQuestionBatchResponse(qs)
		if err != nil {
			m.statusMessage = err.Error()
			m.statusVersion++
			return m.clearStatusAfterDelay(m.statusVersion)
		}
		qs.prompt.ResponseChan <- interaction.QuestionPromptResult{Response: response}
		m.questionPrompt = nil
		m.mode = chatInputMode
	}

	return nil
}

func (m *chatViewModel) handleFileDisplayModeKey(msg tea.KeyMsg) tea.Cmd {
	if m.fileDisplay == nil {
		m.mode = chatInputMode
		return nil
	}

	switch msg.String() {
	case "q", "esc":
		m.fileDisplay.prompt.ResponseChan <- interaction.FileDisplayResult{}
		m.fileDisplay = nil
		m.mode = chatInputMode
		return nil
	case "j", "down":
		m.fileDisplay.viewport.LineDown(1)
	case "k", "up":
		m.fileDisplay.viewport.LineUp(1)
	case "d", "ctrl+d":
		m.fileDisplay.viewport.HalfViewDown()
	case "u", "ctrl+u":
		m.fileDisplay.viewport.HalfViewUp()
	case "pgdown", "ctrl+f":
		m.fileDisplay.viewport.PageDown()
	case "pgup", "ctrl+b":
		m.fileDisplay.viewport.PageUp()
	case "g", "home":
		m.fileDisplay.viewport.GotoTop()
	case "G", "end":
		m.fileDisplay.viewport.GotoBottom()
	}

	return nil
}

func buildQuestionBatchResponse(state *questionPromptState) (*commontypes.QuestionBatchResponse, error) {
	answers := make([]commontypes.QuestionAnswer, 0, len(state.prompt.Request.Questions))

	for idx, q := range state.prompt.Request.Questions {
		a := state.answers[idx]
		custom := strings.TrimSpace(a.customAnswer)
		selectedIdx := a.selectedOption
		selectedLabel := ""
		selectedIndexes := []int{}
		selectedLabels := []string{}

		if q.AllowMultiple {
			for i := range q.Options {
				if a.selectedOptions != nil && a.selectedOptions[i] {
					selectedIndexes = append(selectedIndexes, i)
					selectedLabels = append(selectedLabels, q.Options[i].Label)
				}
			}
			if len(selectedLabels) > 0 {
				selectedLabel = strings.Join(selectedLabels, ", ")
			}
		} else if selectedIdx >= 0 && selectedIdx < len(q.Options) {
			selectedLabel = q.Options[selectedIdx].Label
			selectedIndexes = append(selectedIndexes, selectedIdx)
			selectedLabels = append(selectedLabels, selectedLabel)
		}

		if q.Required && selectedLabel == "" && custom == "" {
			return nil, fmt.Errorf("question %d is required", idx+1)
		}

		finalAnswer := selectedLabel
		if custom != "" {
			finalAnswer = custom
		}

		answer := commontypes.QuestionAnswer{
			ID:                    q.ID,
			SelectedOptionLabel:   selectedLabel,
			SelectedOptionIndexes: selectedIndexes,
			SelectedOptionLabels:  selectedLabels,
			CustomAnswer:          custom,
			FinalAnswer:           finalAnswer,
		}
		if selectedLabel != "" && selectedIdx >= 0 {
			answer.SelectedOptionIndex = &selectedIdx
		}
		answers = append(answers, answer)
	}

	return &commontypes.QuestionBatchResponse{Answers: answers}, nil
}

func (m *chatViewModel) View() string {
	if m.loading || !m.historyLoaded {
		return loadingStyle.Render("Loading conversation...")
	}

	if m.err != nil {
		return errorStyle.Render(fmt.Sprintf("Error: %v\n\nPress ESC to go back", m.err))
	}

	if m.mode == chatModelSelectMode {
		return m.renderModelSelector()
	}

	if m.mode == chatQuestionMode {
		return m.renderQuestionPrompt()
	}

	if m.mode == chatFileDisplayMode {
		return m.renderFileDisplayPrompt()
	}

	spinnerStatus := ""
	if m.hasActiveUserTurn() && len(m.spinnerFrames) > 0 {
		spinnerStatus = sendingStyle.Render(m.spinnerFrames[m.spinnerFrame] + " ")
	}

	status := ""
	if m.statusMessage != "" {
		status = dimStyle.Render(m.statusMessage)
	}

	modeLabel := "Mode: INPUT"
	switch m.mode {
	case chatNormalMode:
		modeLabel = "Mode: NORMAL"
	case chatInputMode:
		modeLabel = "Mode: INPUT"
	}

	currentModel := displayModelName(m.availableModels[m.selectedModelIdx])
	pdfName := "none"
	if strings.TrimSpace(m.selectedPDF) != "" {
		pdfName = filepath.Base(m.selectedPDF)
	}
	modelInfo := fmt.Sprintf("Model: %s", currentModel)
	historyInfo := fmt.Sprintf("History: %d", m.historyCount)
	pdfInfo := fmt.Sprintf("PDF: %s", pdfName)
	skillsInfo := fmt.Sprintf("Skills: %d", len(m.selectedSkills))

	helpText := ""
	if m.mode == chatNormalMode {
		helpText = "i: input • d/u: scroll • g/G: top/bottom • +/-: history • ctrl+g: model • ctrl+a: history • ctrl+t: usage • esc: back"
	} else {
		helpText = "tab/shift+tab: agent • ctrl+n: normal • ctrl+w: send • /auth openai ... • /pdf set|show|clear • /skills list|set|show|clear • ctrl+g: model • ctrl+u/d: scroll • ctrl+a: history • ctrl+t: usage • esc: back"
	}

	agent := m.currentAgent()
	agentLabel := fmt.Sprintf("%sAgent: %s", spinnerStatus, agent.DisplayName)
	agentLabelStyle := dimStyle.Foreground(agentAccentColor(agent.AccentColor))
	textareaStyled := textareaAgentStyle(agent.AccentColor).Render(m.textarea.View())

	mainContent := fmt.Sprintf(
		"%s\n\n%s\n\n%s\n%s\n%s",
		headerStyle.Render(fmt.Sprintf("💬 %s", m.shared.selectedCtx.Name)),
		m.viewport.View(),
		agentLabelStyle.Render(agentLabel),
		textareaStyled,
		status,
	)
	contentWidth := m.contentWidth()
	mainRendered := lipgloss.NewStyle().Width(contentWidth).Render(mainContent)

	if m.showUsagePanel {
		panel := m.renderUsagePanel(helpText, []string{modeLabel, modelInfo, historyInfo, pdfInfo, skillsInfo})
		if panel != "" {
			return lipgloss.JoinHorizontal(
				lipgloss.Top,
				mainRendered,
				usagePanelStyle.Width(usagePanelWidth).Render(panel),
			)
		}
	}

	return mainRendered
}

func (m *chatViewModel) currentAgent() agents.Definition {
	if len(m.availableAgents) == 0 {
		return agents.Definition{Name: "planner", DisplayName: "Planner", AccentColor: "yellow"}
	}
	if m.selectedAgentIdx < 0 || m.selectedAgentIdx >= len(m.availableAgents) {
		return m.availableAgents[0]
	}
	return m.availableAgents[m.selectedAgentIdx]
}

func (m *chatViewModel) selectNextAgent() {
	if len(m.availableAgents) == 0 {
		return
	}
	m.selectedAgentIdx = (m.selectedAgentIdx + 1) % len(m.availableAgents)
}

func (m *chatViewModel) selectPreviousAgent() {
	if len(m.availableAgents) == 0 {
		return
	}
	m.selectedAgentIdx = (m.selectedAgentIdx - 1 + len(m.availableAgents)) % len(m.availableAgents)
}

func composeAgentSystemPrompt(existing string, agent agents.Definition) string {
	existing = strings.TrimSpace(existing)
	agentPrompt := strings.TrimSpace(agent.SystemPrompt)
	if agentPrompt == "" {
		return existing
	}
	section := fmt.Sprintf("## Agent: %s\n%s", agent.Name, agentPrompt)
	if existing == "" {
		return section
	}
	return existing + "\n\n" + section
}

func agentAccentColor(accent string) lipgloss.Color {
	switch strings.ToLower(strings.TrimSpace(accent)) {
	case "yellow":
		return lipgloss.Color("220")
	case "blue":
		return lipgloss.Color("39")
	case "green":
		return lipgloss.Color("42")
	case "cyan":
		return lipgloss.Color("44")
	default:
		return lipgloss.Color("243")
	}
}

func textareaAgentStyle(accent string) lipgloss.Style {
	return lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderLeft(true).
		BorderTop(false).
		BorderRight(false).
		BorderBottom(false).
		BorderForeground(agentAccentColor(accent)).
		PaddingLeft(1)
}

func (m *chatViewModel) renderQuestionPrompt() string {
	if m.questionPrompt == nil {
		return loadingStyle.Render("Loading questions...")
	}

	qs := m.questionPrompt
	q := qs.prompt.Request.Questions[qs.questionIdx]

	var b strings.Builder
	b.WriteString(headerStyle.Render("? "+qs.title) + "\n\n")
	b.WriteString(dimStyle.Render(fmt.Sprintf("Question %d/%d", qs.questionIdx+1, len(qs.prompt.Request.Questions))) + "\n\n")
	b.WriteString(userPromptStyle.Render(q.Question) + "\n\n")
	if len(q.Options) == 0 && q.AllowCustom {
		b.WriteString(dimStyle.Render("No predefined options. Press enter or i to type your answer.") + "\n\n")
	}

	for i, opt := range q.Options {
		cursor := " "
		if i == qs.optionIdx {
			cursor = ">"
		}
		selected := " "
		if q.AllowMultiple {
			if qs.answers[qs.questionIdx].selectedOptions != nil && qs.answers[qs.questionIdx].selectedOptions[i] {
				selected = "x"
			}
		} else if qs.answers[qs.questionIdx].selectedOption == i {
			selected = "x"
		}
		b.WriteString(fmt.Sprintf("%s [%s] %s\n", cursor, selected, opt.Label))
	}

	if q.AllowCustom {
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("Custom answer") + "\n")
		if qs.editingText {
			b.WriteString(qs.customInput.View() + "\n")
		} else {
			custom := strings.TrimSpace(qs.answers[qs.questionIdx].customAnswer)
			if custom == "" {
				custom = "(none)"
			}
			b.WriteString(dimStyle.Render(custom) + "\n")
		}
	}

	b.WriteString("\n")
	selectHint := "enter: select"
	if q.AllowMultiple {
		selectHint = "enter/space: toggle"
	}
	b.WriteString(helpStyle.Render("j/k: option  tab/shift+tab: question  " + selectHint + "  i: custom  x: clear  ctrl+w: submit  esc: cancel"))

	return b.String()
}

func (m *chatViewModel) renderFileDisplayPrompt() string {
	if m.fileDisplay == nil {
		return loadingStyle.Render("Loading file preview...")
	}

	fd := m.fileDisplay
	title := fd.prompt.Title
	if strings.TrimSpace(title) == "" {
		title = "File Preview"
	}

	pathLine := fd.prompt.Path
	if fd.prompt.StartLine > 0 || fd.prompt.EndLine > 0 {
		start := fd.prompt.StartLine
		end := fd.prompt.EndLine
		if start <= 0 {
			start = 1
		}
		if end <= 0 {
			pathLine = fmt.Sprintf("%s (lines %d-end)", pathLine, start)
		} else {
			pathLine = fmt.Sprintf("%s (lines %d-%d)", pathLine, start, end)
		}
	}

	header := headerStyle.Render(title)
	sub := dimStyle.Render(pathLine)
	help := helpStyle.Render("j/k or arrows: scroll • d/u: half-page • pgup/pgdn: page • g/G: top/bottom • q/esc: close")

	return fmt.Sprintf("%s\n%s\n\n%s\n\n%s", header, sub, fd.viewport.View(), help)
}

func (m *chatViewModel) updateViewportContent() {
	if !m.historyLoaded {
		return
	}

	assistantBg := assistantResponseBackgroundColor
	bubbleWidth := m.viewport.Width - 2
	if bubbleWidth < 24 {
		bubbleWidth = 24
	}
	messagePadding := 2
	contentWidth := bubbleWidth - (2 * messagePadding)
	if contentWidth < 20 {
		contentWidth = 20
		bubbleWidth = contentWidth + (2 * messagePadding)
	}

	var b strings.Builder

	for _, h := range m.history {
		pStyle := userPromptStyle
		rStyle := aiResponseStyle
		archivedPrefix := ""
		hasPrompt := strings.TrimSpace(h.Prompt) != ""

		if h.Archived {
			pStyle = dimStyle
			rStyle = dimStyle
			archivedPrefix = "[ARCHIVED] "
		}

		if hasPrompt {
			promptStyle := pStyle.Copy().Width(contentWidth)
			b.WriteString(promptStyle.Render(fmt.Sprintf("%sYou: %s", archivedPrefix, h.Prompt)))
			b.WriteString("\n\n")
		}

		hasResponse := strings.TrimSpace(h.Response) != ""
		if hasResponse {
			rendered := renderMarkdown(h.Response, contentWidth)
			if h.Archived {
				b.WriteString(rStyle.Copy().Width(contentWidth).Render(rendered))
			} else {
				bgRendered := renderWithBackgroundAndPadding(rendered, contentWidth, messagePadding, assistantBg)
				b.WriteString(bgRendered)
			}
		}
		if len(h.ToolUse) > 0 {
			if hasResponse {
				b.WriteString("\n")
			}
			b.WriteString(dimStyle.Render(renderToolUseSummary(h.ToolUse)))
		}
		if hasResponse || len(h.ToolUse) > 0 {
			b.WriteString("\n\n")
		}
	}

	if m.hasActiveUserTurn() && m.currentResponse != "" {
		b.WriteString(userPromptStyle.Copy().Width(contentWidth).Render(fmt.Sprintf("You: %s", m.currentPrompt)))
		b.WriteString("\n\n")

		rendered := renderMarkdown(m.currentResponse, contentWidth)
		bgRendered := renderWithBackgroundAndPadding(rendered, contentWidth, messagePadding, assistantBg)
		b.WriteString(bgRendered)
		b.WriteString("\n")
	}

	m.viewport.SetContent(b.String())
	if m.viewport.Height > 0 && m.viewport.Width > 0 {
		m.viewport.GotoBottom()
	}
}

func renderToolUseSummary(toolUses []data.ToolUse) string {
	if len(toolUses) == 0 {
		return ""
	}

	failed := 0
	for _, tu := range toolUses {
		if !tu.Result.Success {
			failed++
		}
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Tools: %d call(s)", len(toolUses)))
	if failed > 0 {
		b.WriteString(fmt.Sprintf(" (%d failed)", failed))
	}

	maxShown := 3
	for i, tu := range toolUses {
		if i >= maxShown {
			b.WriteString(fmt.Sprintf("\n- ... and %d more", len(toolUses)-maxShown))
			break
		}

		lines := tools.FormatToolUseForDisplay(tu)
		if len(lines) == 0 {
			continue
		}

		b.WriteString(fmt.Sprintf("\n- %s", lines[0]))
		for _, line := range lines[1:] {
			b.WriteString(fmt.Sprintf("\n  %s", line))
		}
	}

	return b.String()
}

func (m *chatViewModel) renderModelSelector() string {
	var b strings.Builder

	b.WriteString(headerStyle.Render("Select Model"))
	b.WriteString("\n\n")

	for i, model := range m.availableModels {
		cursor := " "
		style := itemStyle

		if i == m.modelCursor {
			cursor = ">"
			style = selectedItemStyle
		}

		modelName := displayModelName(model)
		if i == m.selectedModelIdx {
			modelName += " ✓"
		}

		line := fmt.Sprintf("%s %s", cursor, modelName)
		b.WriteString(style.Render(line))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("↑/k up • ↓/j down • enter select • esc/q cancel"))

	return b.String()
}

func displayModelName(model string) string {
	switch model {
	case "gpt":
		return "gpt (responses)"
	case "codex":
		if openai_auth.HasCodexOAuthCredential() {
			return "codex (responses)"
		}
		return "codex (chat completions)"
	case "gpt-chat":
		return "gpt-chat (chat completions)"
	case "codex-chat":
		return "codex-chat (chat completions)"
	default:
		return model
	}
}

func (m *chatViewModel) recalculateUsage() {
	var total commontypes.TokenUsage
	var last *commontypes.TokenUsage
	for _, h := range m.history {
		total.PromptTokens += h.PromptTokens
		total.CompletionTokens += h.CompletionTokens
		total.CacheReadTokens += h.CacheReadTokens
		total.CacheWriteTokens += h.CacheWriteTokens
	}
	if len(m.history) > 0 {
		last = historyToUsage(m.history[len(m.history)-1])
	}
	m.contextUsage = total
	m.lastUsage = last
}

func historyToUsage(h data.History) *commontypes.TokenUsage {
	if h.PromptTokens == 0 && h.CompletionTokens == 0 && h.CacheReadTokens == 0 && h.CacheWriteTokens == 0 {
		return nil
	}
	return &commontypes.TokenUsage{
		PromptTokens:     h.PromptTokens,
		CompletionTokens: h.CompletionTokens,
		CacheReadTokens:  h.CacheReadTokens,
		CacheWriteTokens: h.CacheWriteTokens,
	}
}

func usageLines(u *commontypes.TokenUsage) []string {
	if u == nil {
		return []string{"Prompt: —", "Completion: —", "Cache read: —", "Cache write: —"}
	}
	return []string{
		fmt.Sprintf("Prompt: %d", u.PromptTokens),
		fmt.Sprintf("Completion: %d", u.CompletionTokens),
		fmt.Sprintf("Cache read: %d", u.CacheReadTokens),
		fmt.Sprintf("Cache write: %d", u.CacheWriteTokens),
	}
}

func (m *chatViewModel) renderUsagePanel(helpText string, sessionInfo []string) string {
	var b strings.Builder
	b.WriteString(usagePanelTitleStyle.Render("Session"))
	b.WriteString("\n")
	for _, line := range sessionInfo {
		b.WriteString(usageMetricValueStyle.Render(line))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(usagePanelTitleStyle.Render("Token Usage"))
	b.WriteString("\n")
	b.WriteString(usageMetricLabelStyle.Render("Context Total"))
	b.WriteString("\n")
	for _, line := range usageLines(&m.contextUsage) {
		b.WriteString(usageMetricValueStyle.Render(line))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(usageMetricLabelStyle.Render("Last Message"))
	b.WriteString("\n")
	for _, line := range usageLines(m.lastUsage) {
		b.WriteString(usageMetricValueStyle.Render(line))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(usageMetricLabelStyle.Render("Controls"))
	b.WriteString("\n")
	compactHelpStyle := helpStyle.Copy().MarginTop(0)
	for _, item := range splitHelpItems(helpText) {
		b.WriteString(compactHelpStyle.Render(item))
		b.WriteString("\n")
	}
	b.WriteString(usageMetricLabelStyle.Render("Toggle: ctrl+t"))
	return strings.TrimSuffix(b.String(), "\n")
}

func splitHelpItems(helpText string) []string {
	parts := strings.Split(helpText, " • ")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		items = append(items, trimmed)
	}
	if len(items) == 0 {
		return []string{helpText}
	}
	return items
}

func (m *chatViewModel) scrollHalfPage(down bool) {
	amount := m.viewport.Height / 2
	if amount < 1 {
		amount = 1
	}
	yOffset := m.viewport.YOffset
	if down {
		yOffset += amount
	} else {
		yOffset -= amount
		if yOffset < 0 {
			yOffset = 0
		}
	}
	m.viewport.SetYOffset(yOffset)
}

func (m *chatViewModel) contentWidth() int {
	width := m.width
	if m.showUsagePanel {
		width -= usagePanelWidth + 2
	}
	if width < minContentWidth {
		width = minContentWidth
	}
	return width
}

func (m *chatViewModel) applyLayout() {
	contentWidth := m.contentWidth()
	m.viewport.Width = contentWidth
	viewportHeight := m.height - 10
	if viewportHeight < 0 {
		viewportHeight = 0
	}
	m.viewport.Height = viewportHeight
	textareaWidth := contentWidth - 4
	if textareaWidth < minTextareaWidth {
		textareaWidth = minTextareaWidth
	}
	m.textarea.SetWidth(textareaWidth)
}

func (m *chatViewModel) persistAgentPreference() {
	if m.shared == nil || m.shared.selectedCtx == nil {
		return
	}
	agent := m.currentAgent().Name
	if err := m.shared.config.Repository.UpdatePreferredAgent(m.shared.selectedCtx.Id, agent); err != nil {
		logger.Debug.Printf("failed to persist preferred agent: %v", err)
		return
	}
	m.shared.selectedCtx.PreferredAgent = agent
}

func (m *chatViewModel) persistSkillsPreference() {
	if m.shared == nil || m.shared.selectedCtx == nil {
		return
	}
	joined := strings.Join(m.selectedSkills, ",")
	if err := m.shared.config.Repository.UpdatePreferredSkills(m.shared.selectedCtx.Id, joined); err != nil {
		logger.Debug.Printf("failed to persist preferred skills: %v", err)
		return
	}
	m.shared.selectedCtx.PreferredSkills = joined
}

func parseCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	seen := map[string]bool{}
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		result = append(result, name)
	}
	return result
}

func composePromptSections(parts ...string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		filtered = append(filtered, trimmed)
	}
	return strings.Join(filtered, "\n\n")
}

func availableSkillNames() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("could not resolve home directory: %w", err)
	}
	dir := filepath.Join(home, ".owl", "skills")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to read skills directory: %w", err)
	}

	result := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(strings.ToLower(name), ".md") {
			result = append(result, strings.TrimSuffix(name, filepath.Ext(name)))
		}
	}
	sort.Strings(result)
	return result, nil
}

func missingSkills(selected []string) []string {
	available, err := availableSkillNames()
	if err != nil {
		return selected
	}
	set := map[string]bool{}
	for _, name := range available {
		set[name] = true
	}
	missing := []string{}
	for _, name := range selected {
		if !set[name] {
			missing = append(missing, name)
		}
	}
	return missing
}

func loadSkillsPromptByNames(skillNames []string) string {
	if len(skillNames) == 0 {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		logger.Debug.Printf("skills load: could not resolve home directory: %v", err)
		return ""
	}
	dir := filepath.Join(home, ".owl", "skills")
	b := strings.Builder{}
	for _, skill := range skillNames {
		name := strings.TrimSpace(skill)
		if name == "" {
			continue
		}
		path := filepath.Join(dir, filepath.Base(name)+".md")
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			logger.Debug.Printf("skill load failure for %s: %v", path, readErr)
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(fmt.Sprintf("## Skill: %s\n%s", name, strings.TrimSpace(string(content))))
	}
	return b.String()
}

func renderMarkdown(content string, width int) string {
	if width <= 0 {
		width = 80
	}
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithColorProfile(termenv.TrueColor),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		logger.Debug.Printf("failed to create markdown renderer: %v", err)
		return content
	}
	rendered, err := renderer.Render(content)
	if err != nil {
		logger.Debug.Printf("failed to render markdown: %v", err)
		return content
	}
	return strings.TrimSuffix(rendered, "\n")
}

type tuiResponseHandler struct {
	responseChan              chan string
	exchangeCompletionChannel chan struct{}
	exchangeEventChannel      chan exchangeEventType
	fullResponse              string
	Repository                data.HistoryRepository
	exchangeCompletionOnce    sync.Once
}

func (h *tuiResponseHandler) RecievedText(text string, color *string) {
	h.fullResponse += text
	select {
	case <-h.exchangeCompletionChannel:
		return
	default:
	}

	select {
	case h.responseChan <- text:
	case <-h.exchangeCompletionChannel:
	default:
		// Drop UI chunk if the queue is full to avoid blocking the stream goroutine.
		// fullResponse still keeps the complete text for persistence.
	}
}

func (h *tuiResponseHandler) FinalText(contextId int64, prompt string, response string, toolUse []data.ToolUse, modelName string, usage *commontypes.TokenUsage) {
	persistedResponse := response
	if strings.TrimSpace(persistedResponse) == "" && strings.TrimSpace(h.fullResponse) != "" {
		persistedResponse = h.fullResponse
	}

	history := data.History{
		ContextId:       contextId,
		Prompt:          prompt,
		Response:        persistedResponse,
		ResponseContent: h.fullResponse,
		Abbreviation:    "",
		TokenCount:      0,
		Model:           modelName,
		ToolUse:         toolUse,
	}

	if usage != nil {
		history.PromptTokens = usage.PromptTokens
		history.CompletionTokens = usage.CompletionTokens
		history.CacheReadTokens = usage.CacheReadTokens
		history.CacheWriteTokens = usage.CacheWriteTokens
	}

	_, err := h.Repository.InsertHistory(history)
	if err != nil {
		println(fmt.Sprintf("Error while trying to save history: %s", err))
	} else {
		logger.HistoryPersisted(contextId)
	}

	code := services.ExtractCodeBlocks(persistedResponse)
	allCode := strings.Join(code, "\n\n")

	err = clipboard.WriteAll(allCode)
	if err != nil {
		fmt.Printf("Error copying to clipboard: %v\n", err)
	}

	logger.Debug.Println("Final text in tui response channel")
	select {
	case h.exchangeEventChannel <- exchangeEventStepCompleted:
	default:
	}
	if len(toolUse) == 0 {
		logger.Debug.Println("exchange completed (no tools); signaling via completion channel")
		logger.Debug.Println("closing exchangeCompletionChannel")
		h.exchangeCompletionOnce.Do(func() {
			close(h.exchangeCompletionChannel)
		})
	} else {
		logger.Debug.Println("keeping exchangeCompletionChannel open for tool-call continuation")
	}
}
