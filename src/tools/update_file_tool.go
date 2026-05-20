package tools

import (
	"fmt"
	"os"
	"os/exec"
	"owl/data"
	"owl/logger"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/fatih/color"
)

var unifiedDiffHunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

type FileUpdateTool struct {
	RequireApproval bool // Set to true to require user approval before applying changes
}

func (tool *FileUpdateTool) SetHistory(repo *data.HistoryRepository, context *data.Context) {
}

func (tool *FileUpdateTool) Run(i map[string]string) (string, error) {
	fileName, ok := i["FileName"]
	if !ok {
		return "", fmt.Errorf("FileName parameter is required")
	}

	logger.Screen(fmt.Sprintf("\nAsked to update file %v", fileName), color.RGB(150, 150, 150))

	// Safety checks - same as write_file
	if strings.Contains(fileName, "..") {
		return "", fmt.Errorf("Invalid path '%s' - parent directory references not allowed", fileName)
	}
	if strings.HasPrefix(fileName, "/") {
		return "", fmt.Errorf("Invalid path '%s' - root not allowed", fileName)
	}
	if strings.HasPrefix(fileName, "~") {
		return "", fmt.Errorf("Invalid path '%s' - home not allowed", fileName)
	}

	// Check if file exists
	if _, err := os.Stat(fileName); os.IsNotExist(err) {
		return "", fmt.Errorf("File '%s' does not exist. Use write_file to create new files", fileName)
	}

	// Check if approval is required (via env var or tool setting)
	requireApproval := tool.RequireApproval || os.Getenv("OWL_REQUIRE_APPROVAL") == "true"

	diff, ok := i["Diff"]
	if !ok || diff == "" {
		return "", fmt.Errorf("Diff parameter is required")
	}

	if err := validateUnifiedDiff(diff); err != nil {
		artifactPath, writeErr := persistFailedDiffArtifact(fileName, diff, "validation", "", "", err)
		if writeErr != nil {
			return "", fmt.Errorf("Invalid unified diff: %w (also failed to persist failed diff: %v)", err, writeErr)
		}
		return "", fmt.Errorf("Invalid unified diff: %w (saved at %s)", err, artifactPath)
	}

	logger.Screen(fmt.Sprintf("\nAsked to apply diff"), color.RGB(150, 150, 150))

	// Show diff for approval if required
	if requireApproval {
		result, err := tool.requestApproval(fileName, diff)
		if err != nil {
			return "", fmt.Errorf("Failed to show approval dialog: %s", err)
		}

		switch result {
		case Approved:
			logger.Screen("✓ User approved changes", color.RGB(0, 255, 0))
		case Rejected:
			return "Changes rejected by user", nil
		case Cancelled:
			return "Operation cancelled by user", nil
		}
	}
	result, err := tool.applyDiff(fileName, diff)
	if err != nil {
		logger.Screen(fmt.Sprintf("Failed applying the diff: %s", err), color.RGB(250, 150, 150))
		return "Operation was unsuccessful", err
	}

	return result, nil
}

// requestApproval shows the diff to the user and asks for approval
func (tool *FileUpdateTool) requestApproval(fileName, diff string) (DiffApprovalResult, error) {
	// Check if we're in a TTY (interactive terminal)
	if !isTerminal() {
		logger.Screen("⚠ Not in interactive terminal, auto-approving", color.RGB(255, 255, 0))
		return Approved, nil
	}

	logger.Screen("\n"+lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFF00")).Render("⚡ Review required - launching diff viewer..."), color.RGB(255, 255, 0))

	// Use the best available viewer
	return ShowDiffWithBestViewer(fileName, diff)
}

// applyDiff applies a unified diff patch to the file
func (tool *FileUpdateTool) applyDiff(fileName, diff string) (string, error) {
	// Create a temporary file for the patch
	logger.Debug.Printf("Received diff to apply for file %s:\n%s", fileName, diff)

	tmpFile, err := os.CreateTemp("", "patch-*.diff")
	if err != nil {
		logger.Screen(fmt.Sprintf("\nfailed to write temporary file"), color.RGB(150, 150, 150))
		return "", fmt.Errorf("Failed to create temporary patch file: %s", err)
	}
	defer os.Remove(tmpFile.Name())

	// Write the diff to the temporary file
	if _, err := tmpFile.WriteString(diff); err != nil {
		tmpFile.Close()
		logger.Screen(fmt.Sprintf("\nfailed to write patch"), color.RGB(150, 150, 150))
		return "", fmt.Errorf("Failed to write patch content: %s", err)
	}
	tmpFile.Close()

	// Try to apply the patch using git apply
	cmd := exec.Command("git", "apply", "--unidiff-zero", tmpFile.Name())
	output, err := cmd.CombinedOutput()

	if err != nil {
		logger.Screen("\nfailed to apply with git apply, testing patch fallback", color.RGB(150, 150, 150))
		gitApplyOutput := string(output)

		// If git apply fails, try with patch command and common strip levels.
		stripLevels := []string{"1", "0"}
		var lastErr error
		var lastOutput []byte
		for _, stripLevel := range stripLevels {
			cmd = exec.Command("patch", "-p"+stripLevel, "-i", tmpFile.Name())
			output, err = cmd.CombinedOutput()
			if err == nil {
				lastErr = nil
				lastOutput = output
				break
			}
			lastErr = err
			lastOutput = output
		}

		if lastErr != nil {
			logger.Screen("\nfailed to apply patch, testing content-based fallback", color.RGB(150, 150, 150))

			if contentFallbackErr := applyDiffByContent(fileName, diff); contentFallbackErr == nil {
				result := fmt.Sprintf("Successfully applied diff to '%s' using content-based fallback", fileName)
				logger.Screen(result, color.RGB(150, 150, 150))
				return result, nil
			}

			logger.Screen("\ncontent-based fallback failed", color.RGB(150, 150, 150))
			artifactPath, writeErr := persistFailedDiffArtifact(fileName, diff, "apply", gitApplyOutput, string(lastOutput), lastErr)
			if writeErr != nil {
				return "", fmt.Errorf(
					"Failed to apply patch. git apply output: %s\npatch output: %s\nerror: %s\n(additionally failed to persist failed diff: %v)",
					gitApplyOutput,
					string(lastOutput),
					lastErr,
					writeErr,
				)
			}
			return "", fmt.Errorf(
				"Failed to apply patch. git apply output: %s\npatch output: %s\nerror: %s\nfailed diff saved at: %s",
				gitApplyOutput,
				string(lastOutput),
				lastErr,
				artifactPath,
			)
		}

		output = lastOutput
	}

	result := fmt.Sprintf("Successfully applied diff to '%s'\n%s", fileName, string(output))
	logger.Screen(result, color.RGB(150, 150, 150))
	return result, nil
}

func applyDiffByContent(fileName, diff string) error {
	bytes, err := os.ReadFile(fileName)
	if err != nil {
		return err
	}

	content := strings.ReplaceAll(string(bytes), "\r\n", "\n")
	fileLines := strings.Split(content, "\n")

	hunks, err := parseUnifiedDiffHunks(diff)
	if err != nil {
		return err
	}

	for _, hunk := range hunks {
		oldLines := make([]string, 0, len(hunk.Body))
		newLines := make([]string, 0, len(hunk.Body))

		for _, line := range hunk.Body {
			if line == "" {
				return fmt.Errorf("empty line in hunk body")
			}
			switch line[0] {
			case ' ':
				oldLines = append(oldLines, line[1:])
				newLines = append(newLines, line[1:])
			case '-':
				oldLines = append(oldLines, line[1:])
			case '+':
				newLines = append(newLines, line[1:])
			case '\\':
				// "\\ No newline at end of file" metadata, ignore
			default:
				return fmt.Errorf("unexpected hunk line prefix %q", string(line[0]))
			}
		}

		if len(oldLines) == 0 {
			return fmt.Errorf("content fallback does not support pure insert hunks")
		}

		start := indexOfSubslice(fileLines, oldLines)
		if start < 0 {
			return fmt.Errorf("could not locate hunk content in target file")
		}

		replaced := make([]string, 0, len(fileLines)-len(oldLines)+len(newLines))
		replaced = append(replaced, fileLines[:start]...)
		replaced = append(replaced, newLines...)
		replaced = append(replaced, fileLines[start+len(oldLines):]...)
		fileLines = replaced
	}

	newContent := strings.Join(fileLines, "\n")
	return os.WriteFile(fileName, []byte(newContent), 0o644)
}

type parsedHunk struct {
	Body []string
}

func parseUnifiedDiffHunks(diff string) ([]parsedHunk, error) {
	diff = strings.ReplaceAll(diff, "\r\n", "\n")
	lines := strings.Split(diff, "\n")

	hunks := []parsedHunk{}
	i := 0
	for i < len(lines) {
		line := lines[i]
		if isHunkHeaderLine(line) {
			h := parsedHunk{Body: []string{}}
			i++
			for i < len(lines) {
				line = lines[i]
				if isHunkHeaderLine(line) || strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") {
					break
				}
				if line != "" {
					h.Body = append(h.Body, line)
				}
				i++
			}
			if len(h.Body) == 0 {
				return nil, fmt.Errorf("empty hunk body")
			}
			hunks = append(hunks, h)
			continue
		}
		i++
	}

	if len(hunks) == 0 {
		return nil, fmt.Errorf("no hunks found")
	}

	return hunks, nil
}

func indexOfSubslice(haystack []string, needle []string) int {
	if len(needle) == 0 || len(needle) > len(haystack) {
		return -1
	}
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if slices.Equal(haystack[i:i+len(needle)], needle) {
			return i
		}
	}
	return -1
}

func (tool *FileUpdateTool) GetName() string {
	return "update_file"
}

func (tool *FileUpdateTool) GetDefinition() (Tool, string) {
	return Tool{
		Name:         tool.GetName(),
		Description:  "Updates specific parts of an existing file using a Git-style unified diff. Cannot create new files - use write_file for that. Path is relative to current working directory. Parent directory references (..) are not allowed for security. Format NEEDS to end with a empty new line",
		Groups:       []ToolGroup{ToolGroupDeveloper},
		Dependencies: []ToolDependency{ToolDependencyLocalExec},

		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"FileName": {
					Type:        "string",
					Description: "The name/path of the file to update (relative to current directory). Never access any parent directory, root, or home. File must already exist.",
				},
				"Diff": {
					Type:        "string",
					Description: "A unified diff format patch to apply. THE FORMAT NEEDS TO ALWAYS END WITH A NEW LINE! Example format:\n--- a/file.txt\n+++ b/file.txt\n@@ -1,3 +1,3 @@\n line1\n-old line\n+new line\n line3\n",
				},
			},
		},
	}, LOCAL
}

func (tool *FileUpdateTool) GetGroups() []ToolGroup {
	return []ToolGroup{ToolGroupDeveloper}
}

func validateUnifiedDiff(diff string) error {
	diff = strings.ReplaceAll(diff, "\r\n", "\n")
	lines := strings.Split(diff, "\n")

	if len(lines) == 0 {
		return fmt.Errorf("diff is empty")
	}

	i := 0
	foundFileSection := false

	for i < len(lines) {
		line := lines[i]

		if line == "" || strings.HasPrefix(line, "diff --git ") || strings.HasPrefix(line, "index ") {
			i++
			continue
		}

		if !strings.HasPrefix(line, "--- ") {
			return fmt.Errorf("line %d: expected file header '---', got %q", i+1, line)
		}

		if i+1 >= len(lines) {
			return fmt.Errorf("line %d: missing matching '+++"+"' file header", i+1)
		}

		next := lines[i+1]
		if !strings.HasPrefix(next, "+++ ") {
			return fmt.Errorf("line %d: expected file header '+++', got %q", i+2, next)
		}

		foundFileSection = true
		i += 2
		hunksForFile := 0

		for i < len(lines) {
			line = lines[i]

			if line == "" || strings.HasPrefix(line, "diff --git ") || strings.HasPrefix(line, "index ") {
				i++
				continue
			}

			if strings.HasPrefix(line, "--- ") {
				break
			}

			if !isHunkHeaderLine(line) {
				return fmt.Errorf("line %d: expected hunk header '@@', got %q", i+1, line)
			}

			if strings.HasPrefix(line, "@@ ") {
				if _, _, err := parseHunkCounts(line, i+1); err != nil {
					return err
				}
			}

			hunksForFile++
			i++

			for i < len(lines) {
				line = lines[i]

				if isHunkHeaderLine(line) || strings.HasPrefix(line, "--- ") {
					break
				}

				if line == "" {
					if i == len(lines)-1 {
						break
					}
					return fmt.Errorf("line %d: malformed hunk body, missing line prefix", i+1)
				}

				if strings.HasPrefix(line, "\\ No newline at end of file") {
					i++
					continue
				}

				switch line[0] {
				case ' ':
				case '-':
				case '+':
				default:
					return fmt.Errorf("line %d: malformed hunk body, unexpected prefix %q", i+1, string(line[0]))
				}

				i++
			}
		}

		if hunksForFile == 0 {
			return fmt.Errorf("line %d: file section has no hunks", i+1)
		}
	}

	if !foundFileSection {
		return fmt.Errorf("diff has no file sections")
	}

	return nil
}

func parseHunkCounts(header string, lineNumber int) (int, int, error) {
	matches := unifiedDiffHunkHeader.FindStringSubmatch(header)
	if matches == nil {
		return 0, 0, fmt.Errorf("line %d: invalid hunk header %q", lineNumber, header)
	}

	oldCount := 1
	newCount := 1

	if matches[2] != "" {
		parsed, err := strconv.Atoi(matches[2])
		if err != nil {
			return 0, 0, fmt.Errorf("line %d: invalid old hunk count in %q", lineNumber, header)
		}
		oldCount = parsed
	}

	if matches[4] != "" {
		parsed, err := strconv.Atoi(matches[4])
		if err != nil {
			return 0, 0, fmt.Errorf("line %d: invalid new hunk count in %q", lineNumber, header)
		}
		newCount = parsed
	}

	return oldCount, newCount, nil
}

func isHunkHeaderLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "@@" {
		return true
	}
	return strings.HasPrefix(line, "@@ ")
}

func persistFailedDiffArtifact(fileName, diff, stage, gitApplyOutput, patchOutput string, applyErr error) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	owlDir := filepath.Join(homeDir, ".owl")
	if err := os.MkdirAll(owlDir, 0o755); err != nil {
		return "", err
	}

	timestamp := time.Now().Format("20060102-150405.000")
	artifactName := fmt.Sprintf("failed-diff-%s-%d.log", timestamp, os.Getpid())
	artifactPath := filepath.Join(owlDir, artifactName)

	content := strings.Builder{}
	content.WriteString("stage: " + stage + "\n")
	content.WriteString("file: " + fileName + "\n")
	content.WriteString("time: " + time.Now().Format(time.RFC3339Nano) + "\n")
	if applyErr != nil {
		content.WriteString("error: " + applyErr.Error() + "\n")
	}
	content.WriteString("\n=== git apply output ===\n")
	content.WriteString(gitApplyOutput)
	content.WriteString("\n\n=== patch output ===\n")
	content.WriteString(patchOutput)
	content.WriteString("\n\n=== diff ===\n")
	content.WriteString(diff)
	if !strings.HasSuffix(diff, "\n") {
		content.WriteString("\n")
	}

	if err := os.WriteFile(artifactPath, []byte(content.String()), 0o644); err != nil {
		return "", err
	}

	logger.Debug.Printf("Saved failed diff artifact to %s", artifactPath)
	return artifactPath, nil
}

// Helper functions
func isTerminal() bool {
	fileInfo, _ := os.Stdout.Stat()
	return (fileInfo.Mode() & os.ModeCharDevice) != 0
}

func init() {
	Register(&FileUpdateTool{})
}
