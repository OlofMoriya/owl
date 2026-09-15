package tui

import (
	"owl/interaction"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func newDiffApprovalTestModel(width, height int) *chatViewModel {
	return &chatViewModel{
		mode:            chatInputMode,
		width:           width,
		height:          height,
		historyLoaded:   true,
		textarea:        textarea.New(),
		viewport:        viewport.New(width, height-10),
		currentPrompt:   "Please fix the UI synchronization bug",
		currentResponse: "I will keep one Bubble Tea program in control of the terminal.",
		activeTurns: map[string]*chatTurn{
			"user-1": {id: "user-1", turnType: turnTypeUser, state: turnStateActive},
		},
	}
}

func diffApprovalTestPrompt() interaction.DiffApprovalPrompt {
	return interaction.DiffApprovalPrompt{
		FileName: "src/tui/chat_view.go",
		Diff: strings.Join([]string{
			"--- a/src/tui/chat_view.go",
			"+++ b/src/tui/chat_view.go",
			"@@ -1,1 +1,1 @@",
			"-old",
			"+new",
			"",
		}, "\n"),
		Summary:      "Keep the existing chat visible while reviewing the change.",
		ResponseChan: make(chan interaction.DiffApprovalResult, 1),
	}
}

func TestDiffApprovalWorkspacePreservesConversationAndSummary(t *testing.T) {
	model := newDiffApprovalTestModel(140, 40)
	prompt := diffApprovalTestPrompt()

	model.beginDiffApproval(prompt)

	if model.mode != chatDiffApprovalMode {
		t.Fatalf("expected diff approval mode, got %v", model.mode)
	}
	if model.diffApproval == nil || !model.diffApproval.wide {
		t.Fatal("expected a wide diff approval workspace")
	}

	view := model.renderDiffApprovalWorkspace()
	for _, expected := range []string{
		"Conversation / rationale",
		"Proposed diff",
		prompt.Summary,
		"one Bubble Tea program",
	} {
		if !strings.Contains(view, expected) {
			t.Fatalf("expected review workspace to contain %q", expected)
		}
	}
	lines := strings.Split(view, "\n")
	if len(lines) > model.height {
		t.Fatalf("review workspace is %d lines high for a %d-line terminal", len(lines), model.height)
	}
	for i, line := range lines {
		if width := lipgloss.Width(line); width > model.width {
			t.Fatalf("review workspace line %d is %d columns wide for a %d-column terminal", i+1, width, model.width)
		}
	}
}

func TestDiffApprovalWorkspaceStacksOnNarrowTerminal(t *testing.T) {
	model := newDiffApprovalTestModel(80, 30)
	model.beginDiffApproval(diffApprovalTestPrompt())

	if model.diffApproval == nil || model.diffApproval.wide {
		t.Fatal("expected a stacked narrow-terminal workspace")
	}
	view := model.renderDiffApprovalWorkspace()
	lines := strings.Split(view, "\n")
	if len(lines) > model.height {
		t.Fatalf("stacked workspace is %d lines high for a %d-line terminal", len(lines), model.height)
	}
	for i, line := range lines {
		if width := lipgloss.Width(line); width > model.width {
			t.Fatalf("stacked workspace line %d is %d columns wide for a %d-column terminal: %q", i+1, width, model.width, line)
		}
	}
}

func TestDiffApprovalImmediatelyReturnsDecisionAndRestoresChat(t *testing.T) {
	tests := []struct {
		name     string
		key      tea.KeyMsg
		expected interaction.DiffApprovalDecision
	}{
		{name: "approve", key: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}}, expected: interaction.DiffApprovalApproved},
		{name: "cancel", key: tea.KeyMsg{Type: tea.KeyEsc}, expected: interaction.DiffApprovalCancelled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := newDiffApprovalTestModel(100, 35)
			prompt := diffApprovalTestPrompt()
			model.beginDiffApproval(prompt)

			model.handleDiffApprovalKey(tt.key)

			if model.mode != chatInputMode || model.diffApproval != nil {
				t.Fatalf("expected chat mode to be restored immediately, mode=%v state=%+v", model.mode, model.diffApproval)
			}
			select {
			case result := <-prompt.ResponseChan:
				if result.Decision != tt.expected {
					t.Fatalf("expected decision %v, got %v", tt.expected, result.Decision)
				}
			case <-time.After(time.Second):
				t.Fatal("decision was not returned until another UI event")
			}
		})
	}
}

func TestDiffApprovalReturnsRejectionReason(t *testing.T) {
	model := newDiffApprovalTestModel(100, 35)
	prompt := diffApprovalTestPrompt()
	model.beginDiffApproval(prompt)

	model.handleDiffApprovalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if !model.diffApproval.capturingReason {
		t.Fatal("expected rejection reason input")
	}
	model.diffApproval.reasonInput.SetValue("the test is missing")
	model.handleDiffApprovalKey(tea.KeyMsg{Type: tea.KeyCtrlW})

	select {
	case result := <-prompt.ResponseChan:
		if result.Decision != interaction.DiffApprovalRejected || result.Reason != "the test is missing" {
			t.Fatalf("unexpected rejection result: %+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("rejection was not returned")
	}
}
