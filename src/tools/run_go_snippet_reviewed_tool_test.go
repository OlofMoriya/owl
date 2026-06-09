package tools

import (
	"strings"
	"testing"
)

func TestRunGoSnippetReviewedTool_ValidateInput(t *testing.T) {
	t.Parallel()

	if err := validateGoSnippetReviewedInput("package main\nfunc main() {}\n"); err != nil {
		t.Fatalf("expected valid code, got error: %v", err)
	}

	if err := validateGoSnippetReviewedInput("func main() {}\n"); err == nil || !strings.Contains(err.Error(), "package main") {
		t.Fatalf("expected package main error, got %v", err)
	}

	if err := validateGoSnippetReviewedInput("package main\n"); err == nil || !strings.Contains(err.Error(), "func main()") {
		t.Fatalf("expected func main error, got %v", err)
	}

	blocked := "package main\n\nimport \"os/exec\"\n\nfunc main() {}\n"
	if err := validateGoSnippetReviewedInput(blocked); err == nil || !strings.Contains(err.Error(), "os/exec") {
		t.Fatalf("expected os/exec block error, got %v", err)
	}
}

func TestRunGoSnippetReviewedTool_ParseTimeout(t *testing.T) {
	t.Parallel()

	timeout, err := parseRunGoSnippetTimeout("")
	if err != nil {
		t.Fatalf("expected default timeout, got error: %v", err)
	}
	if timeout != 15 {
		t.Fatalf("expected default timeout 15, got %d", timeout)
	}

	timeout, err = parseRunGoSnippetTimeout("45")
	if err != nil {
		t.Fatalf("expected capped timeout, got error: %v", err)
	}
	if timeout != 30 {
		t.Fatalf("expected capped timeout 30, got %d", timeout)
	}

	if _, err := parseRunGoSnippetTimeout("0"); err == nil {
		t.Fatalf("expected error for timeout 0")
	}
}

func TestRunGoSnippetReviewedTool_ParseOptionalBool(t *testing.T) {
	t.Parallel()

	value, err := parseOptionalBool("")
	if err != nil || value {
		t.Fatalf("expected empty value to parse as false, got value=%v err=%v", value, err)
	}

	value, err = parseOptionalBool("true")
	if err != nil || !value {
		t.Fatalf("expected true, got value=%v err=%v", value, err)
	}

	value, err = parseOptionalBool("false")
	if err != nil || value {
		t.Fatalf("expected false, got value=%v err=%v", value, err)
	}

	if _, err := parseOptionalBool("notabool"); err == nil {
		t.Fatalf("expected invalid bool error")
	}
}

func TestRunGoSnippetReviewedTool_InputSchemaIncludesReviewOutput(t *testing.T) {
	t.Parallel()

	tool := &RunGoSnippetReviewedTool{}
	definition, _ := tool.GetDefinition()
	prop, ok := definition.InputSchema.Properties["ReviewOutput"]
	if !ok {
		t.Fatalf("expected ReviewOutput property to exist")
	}
	if prop.Type != "boolean" {
		t.Fatalf("expected ReviewOutput to be boolean, got %q", prop.Type)
	}
	if !strings.Contains(prop.Description, "always require review") {
		t.Fatalf("unexpected ReviewOutput description: %q", prop.Description)
	}
}

func TestRunGoSnippetReviewedTool_BuildReviewContent(t *testing.T) {
	t.Parallel()

	code := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"
	review := buildGoSnippetReviewContent(code, 12)

	for _, expected := range []string{
		"Go Snippet Execution Review",
		"yaegi <stdin>",
		"Imports of os/exec are blocked.",
		"TimeoutSeconds: 12",
		"package main",
		"fmt.Println(\"hello\")",
	} {
		if !strings.Contains(review, expected) {
			t.Fatalf("expected review to contain %q, got:\n%s", expected, review)
		}
	}
}

func TestRunGoSnippetReviewedTool_DetectWarnings(t *testing.T) {
	t.Parallel()

	code := "package main\n\nimport (\n\t\"net/http\"\n\t\"os/exec\"\n)\n\nfunc main() {}\n"
	warnings := detectGoSnippetWarnings(code)
	joined := strings.Join(warnings, "\n")

	if !strings.Contains(joined, "imports os/exec") {
		t.Fatalf("expected os/exec warning, got %v", warnings)
	}
	if !strings.Contains(joined, "imports net/http") {
		t.Fatalf("expected net/http warning, got %v", warnings)
	}
}

func TestRunGoSnippetReviewedTool_BuildResult(t *testing.T) {
	t.Parallel()

	result := buildRunGoSnippetResult("hello\n", "warn\n", "/usr/local/bin/yaegi")
	for _, expected := range []string{"command: /usr/local/bin/yaegi", "stdout:\nhello", "stderr:\nwarn"} {
		if !strings.Contains(result, expected) {
			t.Fatalf("expected result to contain %q, got:\n%s", expected, result)
		}
	}
}

func TestRunGoSnippetReviewedTool_DetectSensitiveOutput(t *testing.T) {
	t.Parallel()

	output := strings.Join([]string{
		"Authorization: Bearer abcdefghijklmnopqrstuvwxyz123456",
		"jwt: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTYifQ.signaturepart",
		"-----BEGIN OPENSSH PRIVATE KEY-----",
		"AKIAABCDEFGHIJKLMNOP",
	}, "\n")

	warnings := detectSensitiveOutput(output)
	joined := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		joined = append(joined, warning.Label)
	}
	text := strings.Join(joined, "\n")

	for _, expected := range []string{"ssh private key", "JWT", "bearer token", "AWS access key"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected sensitive warning %q, got %v", expected, joined)
		}
	}
}
