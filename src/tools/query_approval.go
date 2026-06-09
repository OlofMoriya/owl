package tools

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type ApprovalDecision struct {
	Result DiffApprovalResult
	Reason string
}

// ShowQueryApproval shows query text and asks for approval.
// It uses a tmux popup when available, with terminal fallback.
func ShowQueryApproval(title, content string) (ApprovalDecision, error) {
	if strings.TrimSpace(os.Getenv("TMUX")) != "" {
		result, err := showQueryApprovalInTmux(title, content)
		if err == nil {
			return result, nil
		}
	}

	return showQueryApprovalInTerminal(title, content)
}

func ShowThreeWayExecutionApproval(title, content string) (snippetExecutionChoice, error) {
	if strings.TrimSpace(os.Getenv("TMUX")) != "" {
		result, err := showThreeWayExecutionApprovalInTmux(title, content)
		if err == nil {
			return result, nil
		}
	}

	return showThreeWayExecutionApprovalInTerminal(title, content)
}

func showQueryApprovalInTmux(title, content string) (ApprovalDecision, error) {
	if _, err := exec.LookPath("tmux"); err != nil {
		return ApprovalDecision{Result: Cancelled}, fmt.Errorf("tmux is not available: %w", err)
	}

	tmpDir := os.TempDir()
	contentFile := filepath.Join(tmpDir, "owl-dbask-query-preview.txt")
	responseFile := filepath.Join(tmpDir, "owl-dbask-query-response.txt")
	scriptFile := filepath.Join(tmpDir, "owl-dbask-query-approval.sh")

	os.Remove(responseFile)

	if err := os.WriteFile(contentFile, []byte(content), 0644); err != nil {
		return ApprovalDecision{Result: Cancelled}, fmt.Errorf("failed to write query preview file: %w", err)
	}
	defer os.Remove(contentFile)

	script := fmt.Sprintf(`#!/bin/bash
set -u

clear
echo "============================================================"
echo " DBASK QUERY APPROVAL: %s"
echo "============================================================"
echo ""
cat "%s"
echo ""
echo "------------------------------------------------------------"
echo "Approve this query?"
echo "  [y] yes"
echo "  [n] no (with reason)"

while true; do
  echo -n "Your choice: "
  read -n 1 -r REPLY
  echo ""
  case "$REPLY" in
    y|Y)
      printf 'approved\n' > "%s"
      break
      ;;
    n|N)
      echo -n "Reason for denial: "
      IFS= read -r DENIAL_REASON
      {
        printf 'rejected\n'
        printf '%%s\n' "$DENIAL_REASON"
      } > "%s"
      break
      ;;
    *)
      echo "Please type y or n."
      ;;
  esac
done
`, title, contentFile, responseFile, responseFile)

	if err := os.WriteFile(scriptFile, []byte(script), 0755); err != nil {
		return ApprovalDecision{Result: Cancelled}, fmt.Errorf("failed to write approval script: %w", err)
	}
	defer os.Remove(scriptFile)

	cmd := exec.Command(
		"tmux", "display-popup",
		"-E",
		"-w", "90%",
		"-h", "90%",
		"-T", " DBASK Query Approval ",
		scriptFile,
	)

	if err := cmd.Run(); err != nil {
		return ApprovalDecision{Result: Cancelled}, fmt.Errorf("failed to launch tmux popup: %w", err)
	}

	responseBytes, err := os.ReadFile(responseFile)
	if err != nil {
		return ApprovalDecision{Result: Cancelled}, nil
	}
	defer os.Remove(responseFile)

	return parseApprovalDecision(string(responseBytes)), nil
}

func showThreeWayExecutionApprovalInTmux(title, content string) (snippetExecutionChoice, error) {
	if _, err := exec.LookPath("tmux"); err != nil {
		return snippetExecutionCancelled, fmt.Errorf("tmux is not available: %w", err)
	}

	tmpDir := os.TempDir()
	contentFile := filepath.Join(tmpDir, "owl-three-way-preview.txt")
	responseFile := filepath.Join(tmpDir, "owl-three-way-response.txt")
	scriptFile := filepath.Join(tmpDir, "owl-three-way-approval.sh")

	os.Remove(responseFile)

	if err := os.WriteFile(contentFile, []byte(content), 0644); err != nil {
		return snippetExecutionCancelled, fmt.Errorf("failed to write preview file: %w", err)
	}
	defer os.Remove(contentFile)

	script := fmt.Sprintf(`#!/bin/bash
set -u

clear
echo "============================================================"
echo " GO SNIPPET EXECUTION REVIEW: %s"
echo "============================================================"
echo ""
cat "%s"
echo ""
echo "------------------------------------------------------------"
echo "Run code?"
echo "  [y] yes"
echo "  [r] yes, but review output"
echo "  [n] no"

while true; do
  echo -n "Your choice: "
  read -n 1 -r REPLY
  echo ""
  case "$REPLY" in
    y|Y)
      printf 'approve\n' > "%s"
      break
      ;;
    r|R)
      printf 'approve_review_output\n' > "%s"
      break
      ;;
    n|N)
      printf 'reject\n' > "%s"
      break
      ;;
    *)
      echo "Please type y, r, or n."
      ;;
  esac
done
`, title, contentFile, responseFile, responseFile, responseFile)

	if err := os.WriteFile(scriptFile, []byte(script), 0755); err != nil {
		return snippetExecutionCancelled, fmt.Errorf("failed to write approval script: %w", err)
	}
	defer os.Remove(scriptFile)

	cmd := exec.Command(
		"tmux", "display-popup",
		"-E",
		"-w", "90%",
		"-h", "90%",
		"-T", " OWL Snippet Execution Approval ",
		scriptFile,
	)

	if err := cmd.Run(); err != nil {
		return snippetExecutionCancelled, fmt.Errorf("failed to launch tmux popup: %w", err)
	}

	responseBytes, err := os.ReadFile(responseFile)
	if err != nil {
		return snippetExecutionCancelled, nil
	}
	defer os.Remove(responseFile)

	return parseThreeWayExecutionChoice(string(responseBytes)), nil
}

func showQueryApprovalInTerminal(title, content string) (ApprovalDecision, error) {
	fmt.Println("============================================================")
	fmt.Printf(" DBASK QUERY APPROVAL: %s\n", title)
	fmt.Println("============================================================")
	fmt.Println()
	fmt.Println(content)
	fmt.Println()
	fmt.Println("------------------------------------------------------------")
	fmt.Println("Approve this query?")
	fmt.Println("  [y] yes")
	fmt.Println("  [n] no (with reason)")

	for {
		fmt.Print("Your choice: ")
		var response string
		if _, err := fmt.Scanln(&response); err != nil {
			return ApprovalDecision{Result: Cancelled}, err
		}
		switch strings.ToLower(strings.TrimSpace(response)) {
		case "y", "yes":
			return ApprovalDecision{Result: Approved}, nil
		case "n", "no":
			fmt.Print("Reason for denial: ")
			var reason string
			if _, err := fmt.Scanln(&reason); err != nil {
				return ApprovalDecision{Result: Rejected}, nil
			}
			return ApprovalDecision{Result: Rejected, Reason: strings.TrimSpace(reason)}, nil
		default:
			fmt.Println("Please type y or n.")
		}
	}
}

func showThreeWayExecutionApprovalInTerminal(title, content string) (snippetExecutionChoice, error) {
	fmt.Println("============================================================")
	fmt.Printf(" GO SNIPPET EXECUTION REVIEW: %s\n", title)
	fmt.Println("============================================================")
	fmt.Println()
	fmt.Println(content)
	fmt.Println()
	fmt.Println("------------------------------------------------------------")
	fmt.Println("Run code?")
	fmt.Println("  [y] yes")
	fmt.Println("  [r] yes, but review output")
	fmt.Println("  [n] no")

	for {
		fmt.Print("Your choice: ")
		var response string
		if _, err := fmt.Scanln(&response); err != nil {
			return snippetExecutionCancelled, err
		}
		switch strings.ToLower(strings.TrimSpace(response)) {
		case "y", "yes":
			return snippetExecutionApproved, nil
		case "r", "review":
			return snippetExecutionApprovedWithOutputReview, nil
		case "n", "no":
			return snippetExecutionCancelled, nil
		default:
			fmt.Println("Please type y, r, or n.")
		}
	}
}

func parseApprovalDecision(raw string) ApprovalDecision {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(raw, "\n")
	status := strings.TrimSpace(lines[0])
	reason := ""
	if len(lines) > 1 {
		reason = strings.TrimSpace(strings.Join(lines[1:], "\n"))
	}

	switch status {
	case "approved":
		return ApprovalDecision{Result: Approved}
	case "rejected":
		return ApprovalDecision{Result: Rejected, Reason: reason}
	default:
		return ApprovalDecision{Result: Cancelled}
	}
}

func parseThreeWayExecutionChoice(raw string) snippetExecutionChoice {
	switch strings.TrimSpace(raw) {
	case "approve":
		return snippetExecutionApproved
	case "approve_review_output":
		return snippetExecutionApprovedWithOutputReview
	default:
		return snippetExecutionCancelled
	}
}
