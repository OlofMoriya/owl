package tools

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// TmuxDiffViewer shows diffs in a tmux popup window
type TmuxDiffViewer struct{}

// ShowDiffInTmux displays the diff in a tmux popup and returns user's decision
func ShowDiffInTmux(fileName, diff string) (ApprovalDecision, error) {
	// Check if we're in a tmux session
	if os.Getenv("TMUX") == "" {
		return ApprovalDecision{Result: Cancelled}, fmt.Errorf("not in a tmux session")
	}

	// Create temporary files for the diff and the response
	tmpDir := os.TempDir()
	diffFile := filepath.Join(tmpDir, "owl-diff-preview.diff")
	responseFile := filepath.Join(tmpDir, "owl-diff-response.txt")
	scriptFile := filepath.Join(tmpDir, "owl-diff-viewer.sh")

	// Clean up old response file
	os.Remove(responseFile)

	// Write the diff to a file
	if err := os.WriteFile(diffFile, []byte(diff), 0644); err != nil {
		return ApprovalDecision{Result: Cancelled}, fmt.Errorf("failed to write diff file: %s", err)
	}
	defer os.Remove(diffFile)

	// Create a script that displays the diff and asks for approval
	script := fmt.Sprintf(`#!/bin/bash

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

clear

echo -e "${BLUE}╔════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║${NC}  ${YELLOW}📝 File Update Preview: %s${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════════════╝${NC}"
echo ""

# Get terminal width
WIDTH=$(tput cols 2>/dev/null || echo "120")

# Show the diff with colors using delta, bat, or fallback
if command -v delta &> /dev/null; then
    if [ "$WIDTH" -gt 160 ]; then
        cat "%s" | delta --line-numbers --side-by-side
    else
        cat "%s" | delta --line-numbers
    fi
elif command -v bat &> /dev/null; then
    bat --style=grid --color=always --language=diff "%s"
else
    cat "%s" | sed \
        -e 's/^+++.*/\x1b[1;36m&\x1b[0m/' \
        -e 's/^---.*/\x1b[1;36m&\x1b[0m/' \
        -e 's/^+.*/\x1b[32m&\x1b[0m/' \
        -e 's/^-.*/\x1b[31m&\x1b[0m/' \
        -e 's/^@@.*/\x1b[36m&\x1b[0m/' | less -R
fi

echo ""
echo -e "${BLUE}────────────────────────────────────────────────────────────${NC}"
echo ""
echo -e "  ${GREEN}[y]${NC} Approve and apply changes"
echo -e "  ${RED}[n]${NC} Reject changes (with reason)"
echo -e "  ${YELLOW}[q]${NC} Cancel operation"

while true; do
echo ""
echo -n "Your choice: "

read -n 1 -r REPLY
echo ""

	case "$REPLY" in
		y|Y)
			printf 'approved\n' > "%s"
			echo -e "\n${GREEN}✓ Changes approved!${NC}"
			break
			;;
		n|N)
			echo -n "Reason for denial: "
			IFS= read -r DENIAL_REASON
			{
				printf 'rejected\n'
				printf '%%s\n' "$DENIAL_REASON"
			} > "%s"
			echo -e "\n${RED}✗ Changes rejected!${NC}"
            break
			;;
		c|C|q|Q)
			printf 'cancelled\n' > "%s"
			echo -e "\n${YELLOW}⚠ Operation cancelled!${NC}"
			break
			;;
		*)
		    echo "invalid choice"
			;;
	esac
done

sleep 1
`, fileName, diffFile, diffFile, diffFile, diffFile, responseFile, responseFile, responseFile)

	if err := os.WriteFile(scriptFile, []byte(script), 0755); err != nil {
		return ApprovalDecision{Result: Cancelled}, fmt.Errorf("failed to write script file: %s", err)
	}
	defer os.Remove(scriptFile)

	cmd := exec.Command("tmux", "display-popup",
		"-E",
		"-w", "90%",
		"-h", "90%",
		"-T", " OWL Diff Approval ",
		scriptFile)

	if err := cmd.Run(); err != nil {
		return ApprovalDecision{Result: Cancelled}, fmt.Errorf("failed to run tmux popup: %s", err)
	}

	responseBytes, err := os.ReadFile(responseFile)
	if err != nil {
		return ApprovalDecision{Result: Cancelled}, nil
	}
	defer os.Remove(responseFile)

	return parseApprovalDecision(string(responseBytes)), nil
}

// ShowDiffInTerminal shows diff using less or bat in the current terminal
func ShowDiffInTerminal(fileName, diff string) (ApprovalDecision, error) {
	// Create temporary file for the diff
	tmpFile, err := os.CreateTemp("", "owl-diff-*.diff")
	if err != nil {
		return ApprovalDecision{Result: Cancelled}, fmt.Errorf("failed to create temp file: %s", err)
	}
	defer os.Remove(tmpFile.Name())

	// Write header
	header := fmt.Sprintf(`
╔════════════════════════════════════════════════════════════╗
║  📝 File Update Preview: %s
╚════════════════════════════════════════════════════════════╝

`, fileName)

	tmpFile.WriteString(header)
	tmpFile.WriteString(diff)
	tmpFile.Close()

	if _, err := exec.LookPath("delta"); err == nil {
		cmd := exec.Command("sh", "-c", fmt.Sprintf("cat '%s' | delta --paging=always", tmpFile.Name()))
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		cmd.Run()
	} else if _, err := exec.LookPath("bat"); err == nil {
		cmd := exec.Command("bat", "--style=grid", "--color=always", "--language=diff", tmpFile.Name())
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		cmd.Run()
	} else {
		cmd := exec.Command("sh", "-c", fmt.Sprintf("cat '%s' | sed -e 's/^+++.*/\\x1b[1;36m&\\x1b[0m/' -e 's/^---.*/\\x1b[1;36m&\\x1b[0m/' -e 's/^+.*/\\x1b[32m&\\x1b[0m/' -e 's/^-.*/\\x1b[31m&\\x1b[0m/' -e 's/^@@.*/\\x1b[36m&\\x1b[0m/' | less -R", tmpFile.Name()))
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		cmd.Run()
	}

	fmt.Println("\n────────────────────────────────────────────────────────────")
	fmt.Println("\n  \033[32m[y]\033[0m Approve and apply changes")
	fmt.Println("  \033[31m[n]\033[0m Reject changes (with reason)")
	fmt.Println("  \033[33m[q]\033[0m Cancel operation")
	fmt.Print("\nYour choice: ")

	var response string
	fmt.Scanln(&response)

	response = strings.ToLower(strings.TrimSpace(response))
	switch response {
	case "y", "yes":
		return ApprovalDecision{Result: Approved}, nil
	case "n", "no":
		fmt.Print("Reason for denial: ")
		var reason string
		fmt.Scanln(&reason)
		return ApprovalDecision{Result: Rejected, Reason: strings.TrimSpace(reason)}, nil
	default:
		return ApprovalDecision{Result: Cancelled}, nil
	}
}

// DetectBestViewer returns the best available diff viewer
func DetectBestViewer() string {
	if os.Getenv("TMUX") != "" {
		return "tmux"
	}

	if isTerminal() {
		return "bubbletea"
	}

	return "terminal"
}

// ShowDiffWithBestViewer automatically selects the best viewer
func ShowDiffWithBestViewer(fileName, diff string) (ApprovalDecision, error) {
	viewer := os.Getenv("OWL_DIFF_VIEWER")
	if viewer == "" {
		viewer = DetectBestViewer()
	}

	switch viewer {
	case "tmux":
		result, err := ShowDiffInTmux(fileName, diff)
		if err == nil {
			return result, nil
		}
		fallthrough
	case "bubbletea":
		return ShowDiffForApproval(fileName, diff)
	case "terminal":
		return ShowDiffInTerminal(fileName, diff)
	default:
		return ShowDiffForApproval(fileName, diff)
	}
}
