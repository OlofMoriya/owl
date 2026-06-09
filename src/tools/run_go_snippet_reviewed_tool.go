package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os/exec"
	"owl/data"
	"owl/logger"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
)

type sensitiveOutputWarning struct {
	Label string
}

type snippetExecutionChoice int

const (
	snippetExecutionCancelled snippetExecutionChoice = iota
	snippetExecutionApproved
	snippetExecutionApprovedWithOutputReview
)

var (
	sshPrivateKeyPattern  = regexp.MustCompile(`-----BEGIN (?:OPENSSH|RSA|EC|DSA|PRIVATE) PRIVATE KEY-----`)
	jwtPattern            = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`)
	bearerTokenPattern    = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/-]{16,}`)
	awsAccessKeyPattern   = regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)
	keyValueSecretPattern = regexp.MustCompile(`(?i)\b(api[_-]?key|access[_-]?token|refresh[_-]?token|client[_-]?secret|auth[_-]?token|token|secret)\b\s*[:=]\s*['"]?[A-Za-z0-9._~+/-]{16,}`)
)

type RunGoSnippetReviewedTool struct{}

func (tool *RunGoSnippetReviewedTool) SetHistory(repo *data.HistoryRepository, context *data.Context) {
}

func (tool *RunGoSnippetReviewedTool) GetName() string {
	return "run_go_snippet_reviewed"
}

func (tool *RunGoSnippetReviewedTool) GetGroups() []ToolGroup {
	return []ToolGroup{ToolGroupDeveloper, ToolGroupPlanner, ToolGroupManager}
}

func (tool *RunGoSnippetReviewedTool) GetDefinition() (Tool, string) {
	return Tool{
		Name:        tool.GetName(),
		Description: "Safely runs a Go snippet with the local yaegi CLI only after mandatory human review. Always pass the complete Go source in Code so the entire snippet is preserved in history. Imports of os/exec are blocked.",
		Groups:      []ToolGroup{ToolGroupDeveloper, ToolGroupPlanner, ToolGroupManager},
		Dependencies: []ToolDependency{
			ToolDependencyLocalExec,
		},
		InputSchema: InputSchema{
			Type:     "object",
			Required: []string{"Code"},
			Properties: map[string]Property{
				"Code": {
					Type:        "string",
					Description: "Full Go source code to execute. Must be the full program source, not a shell heredoc. The entire source is stored in history.",
				},
				"TimeoutSeconds": {
					Type:        "integer",
					Description: "Optional execution timeout in seconds. Defaults to 15, max 30.",
				},
				"ReviewOutput": {
					Type:        "boolean",
					Description: "Optional override. If true, always require review of stdout/stderr before sending tool output back to the model. If false, output review is triggered only when possible secrets are detected. If omitted, the user chooses at execution time: yes, yes but review output, or no.",
				},
			},
		},
	}, LOCAL
}

func (tool *RunGoSnippetReviewedTool) Run(i map[string]string) (string, error) {
	code := strings.TrimSpace(i["Code"])
	if code == "" {
		return "", fmt.Errorf("Code is required")
	}

	if !isTerminal() {
		return "", fmt.Errorf("run_go_snippet_reviewed requires interactive review; refusing execution outside interactive terminal")
	}

	if err := validateGoSnippetReviewedInput(code); err != nil {
		return "", err
	}

	timeoutSeconds, err := parseRunGoSnippetTimeout(i["TimeoutSeconds"])
	if err != nil {
		return "", err
	}

	review := buildGoSnippetReviewContent(code, timeoutSeconds)
	executionChoice, reviewOutputOverride, err := resolveExecutionChoice(i, review)
	if err != nil {
		return "", err
	}

	switch executionChoice {
	case snippetExecutionCancelled:
		return "Execution cancelled by user", nil
	case snippetExecutionApproved, snippetExecutionApprovedWithOutputReview:
		logger.Screen("\n✓ User approved Go snippet execution\n", color.RGB(0, 255, 0))
	}

	reviewOutput := executionChoice == snippetExecutionApprovedWithOutputReview
	if reviewOutputOverride != nil {
		reviewOutput = *reviewOutputOverride
	}

	logger.Screen("\nAsked to execute reviewed Go snippet\n", color.RGB(150, 150, 150))

	yaegiPath, err := exec.LookPath("yaegi")
	if err != nil {
		return "", fmt.Errorf("yaegi CLI is required but was not found in PATH")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, yaegiPath)
	cmd.Stdin = strings.NewReader(code)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	result := buildRunGoSnippetResult(stdout.String(), stderr.String(), cmd.Path)
	warnings := detectSensitiveOutput(result)

	if ctx.Err() == context.DeadlineExceeded {
		return result, fmt.Errorf("execution timed out after %d seconds", timeoutSeconds)
	}
	if err != nil {
		reviewedResult, reviewErr := maybeReviewCommandOutput(result, reviewOutput, warnings)
		if reviewErr != nil {
			return "", reviewErr
		}
		return reviewedResult, fmt.Errorf("yaegi execution failed: %w", err)
	}

	return maybeReviewCommandOutput(result, reviewOutput, warnings)
}

func resolveExecutionChoice(input map[string]string, review string) (snippetExecutionChoice, *bool, error) {
	questionDecision, err := ShowThreeWayExecutionApproval("Go Snippet Execution Review", review)
	if err != nil {
		return snippetExecutionCancelled, nil, fmt.Errorf("failed to show execution choice dialog: %w", err)
	}
	return questionDecision, nil, nil
}

func (tool *RunGoSnippetReviewedTool) FormatToolUse(toolUse data.ToolUse) []string {
	input := ParseToolUseInput(toolUse)
	status := "✓"
	if !toolUse.Result.Success {
		status = "✗"
	}

	code := input["Code"]
	codeLines := strings.Split(strings.ReplaceAll(code, "\r\n", "\n"), "\n")
	lines := []string{fmt.Sprintf("run_go_snippet_reviewed %s", status)}

	if timeout := strings.TrimSpace(input["TimeoutSeconds"]); timeout != "" {
		lines = append(lines, fmt.Sprintf("timeout_seconds: %s", timeout))
	}
	if reviewOutput := strings.TrimSpace(input["ReviewOutput"]); reviewOutput != "" {
		lines = append(lines, fmt.Sprintf("review_output: %s", reviewOutput))
	}

	hash := sha256.Sum256([]byte(code))
	lines = append(lines,
		fmt.Sprintf("sha256: %s", hex.EncodeToString(hash[:])),
		fmt.Sprintf("code_lines: %d", len(codeLines)),
		"code:",
	)

	for _, line := range codeLines {
		lines = append(lines, "    "+line)
	}

	resultPreview := strings.TrimSpace(toolUse.Result.Content)
	if resultPreview != "" {
		lines = append(lines, fmt.Sprintf("result: %s", singleLine(resultPreview, 120)))
	}

	return lines
}

func validateGoSnippetReviewedInput(code string) error {
	if !strings.Contains(code, "package main") {
		return fmt.Errorf("code must contain package main")
	}
	if !strings.Contains(code, "func main(") {
		return fmt.Errorf("code must contain func main()")
	}
	if strings.Contains(code, `"os/exec"`) || strings.Contains(code, "`os/exec`") {
		return fmt.Errorf("imports of os/exec are blocked")
	}
	return nil
}

func parseRunGoSnippetTimeout(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 15, nil
	}

	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("TimeoutSeconds must be a positive integer")
	}
	if n < 1 {
		return 0, fmt.Errorf("TimeoutSeconds must be >= 1")
	}
	if n > 30 {
		n = 30
	}
	return n, nil
}

func parseOptionalBool(raw string) (bool, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false, nil
	}

	var value bool
	if err := json.Unmarshal([]byte(trimmed), &value); err == nil {
		return value, nil
	}

	switch strings.ToLower(trimmed) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("ReviewOutput must be a boolean")
	}
}

func buildGoSnippetReviewContent(code string, timeoutSeconds int) string {
	hash := sha256.Sum256([]byte(code))
	hashText := hex.EncodeToString(hash[:])
	warnings := detectGoSnippetWarnings(code)

	var builder strings.Builder
	builder.WriteString("Go Snippet Execution Review\n\n")
	builder.WriteString("Executor:\n")
	builder.WriteString("  yaegi <stdin>\n")
	builder.WriteString("  Imports of os/exec are blocked.\n\n")
	if len(warnings) > 0 {
		builder.WriteString("  Risk markers detected:\n")
		for _, warning := range warnings {
			builder.WriteString("    - ")
			builder.WriteString(warning)
			builder.WriteString("\n")
		}
	}
	builder.WriteString("\nMetadata:\n")
	builder.WriteString(fmt.Sprintf("  SHA256: %s\n", hashText))
	builder.WriteString(fmt.Sprintf("  TimeoutSeconds: %d\n", timeoutSeconds))
	builder.WriteString(fmt.Sprintf("  Lines: %d\n\n", len(strings.Split(strings.ReplaceAll(code, "\r\n", "\n"), "\n"))))
	builder.WriteString("Full code:\n")
	builder.WriteString("--------------------------------------------------\n")
	builder.WriteString(code)
	if !strings.HasSuffix(code, "\n") {
		builder.WriteString("\n")
	}
	builder.WriteString("--------------------------------------------------\n")
	return builder.String()
}

func buildRunGoSnippetResult(stdout, stderr, command string) string {
	stdout = strings.TrimSpace(stdout)
	stderr = strings.TrimSpace(stderr)

	sections := []string{fmt.Sprintf("command: %s", command)}
	if stdout != "" {
		sections = append(sections, "stdout:\n"+stdout)
	}
	if stderr != "" {
		sections = append(sections, "stderr:\n"+stderr)
	}
	if stdout == "" && stderr == "" {
		sections = append(sections, "stdout:\n<empty>")
	}

	return strings.Join(sections, "\n\n")
}

func detectSensitiveOutput(text string) []sensitiveOutputWarning {
	warnings := make([]sensitiveOutputWarning, 0)
	seen := map[string]bool{}

	add := func(label string) {
		if seen[label] {
			return
		}
		seen[label] = true
		warnings = append(warnings, sensitiveOutputWarning{Label: label})
	}

	if sshPrivateKeyPattern.MatchString(text) {
		add("ssh private key")
	}
	if jwtPattern.MatchString(text) {
		add("JWT")
	}
	if bearerTokenPattern.MatchString(text) {
		add("bearer token")
	}
	if awsAccessKeyPattern.MatchString(text) {
		add("AWS access key")
	}
	if keyValueSecretPattern.MatchString(text) {
		add("token/secret value")
	}

	return warnings
}

func maybeReviewCommandOutput(result string, forceReview bool, warnings []sensitiveOutputWarning) (string, error) {
	if !forceReview && len(warnings) == 0 {
		return result, nil
	}

	var builder strings.Builder
	builder.WriteString("Execution Output Review\n\n")
	builder.WriteString("This review happens before tool output is sent back to the model.\n\n")
	if forceReview {
		builder.WriteString("Trigger:\n  user requested output review\n\n")
	}
	if len(warnings) > 0 {
		builder.WriteString("Sensitive output warnings:\n")
		for _, warning := range warnings {
			builder.WriteString("  - ")
			builder.WriteString(warning.Label)
			builder.WriteString("\n")
		}
		builder.WriteString("\n")
	}
	builder.WriteString("Output:\n")
	builder.WriteString("--------------------------------------------------\n")
	builder.WriteString(result)
	if !strings.HasSuffix(result, "\n") {
		builder.WriteString("\n")
	}
	builder.WriteString("--------------------------------------------------\n")

	decision, err := ShowQueryApproval("Go Snippet Output Review", builder.String())
	if err != nil {
		return "", fmt.Errorf("failed to show output review dialog: %w", err)
	}

	switch decision.Result {
	case Approved:
		return result, nil
	case Rejected:
		if strings.TrimSpace(decision.Reason) != "" {
			return fmt.Sprintf("Execution completed, but output was withheld by user before being sent to the model. Reason: %s", decision.Reason), nil
		}
		return "Execution completed, but output was withheld by user before being sent to the model.", nil
	default:
		return "Execution completed, but output was withheld by user before being sent to the model.", nil
	}
}

func detectGoSnippetWarnings(code string) []string {
	checks := []struct {
		needle  string
		warning string
	}{
		{`"os/exec"`, `imports os/exec`},
		{`"syscall"`, `imports syscall`},
		{`"unsafe"`, `imports unsafe`},
		{`"plugin"`, `imports plugin`},
		{`"net"`, `imports net`},
		{`"net/http"`, `imports net/http`},
		{`import "C"`, `uses cgo via import "C"`},
	}

	warnings := make([]string, 0)
	for _, check := range checks {
		if strings.Contains(code, check.needle) {
			warnings = append(warnings, check.warning)
		}
	}
	return warnings
}

func init() {
	Register(&RunGoSnippetReviewedTool{})
}
