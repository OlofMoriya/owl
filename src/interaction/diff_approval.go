package interaction

// DiffApprovalDecision is the user's decision for a proposed file change.
type DiffApprovalDecision int

const (
	DiffApprovalApproved DiffApprovalDecision = iota
	DiffApprovalRejected
	DiffApprovalCancelled
)

// DiffApprovalPrompt asks the active TUI to review a proposed file change.
type DiffApprovalPrompt struct {
	FileName     string
	Diff         string
	Summary      string
	ResponseChan chan DiffApprovalResult
}

// DiffApprovalResult returns the user's decision to the waiting tool call.
type DiffApprovalResult struct {
	Decision DiffApprovalDecision
	Reason   string
}

// DiffApprovalPromptChan is initialized while Owl's integrated TUI is running.
var DiffApprovalPromptChan chan DiffApprovalPrompt
