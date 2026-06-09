package tui

import (
	"strings"
	"testing"
)


func TestExtractReviewedGoSnippetCode(t *testing.T) {
	raw := `{"Code":"package main\n\nimport \"fmt\"\n\nfunc main() {\n    fmt.Println(\"hello\")\n}\n","TimeoutSeconds":10}`
	code := extractReviewedGoSnippetCode(raw)
	if !strings.Contains(code, "package main") {
		t.Fatalf("expected extracted code to contain package main, got: %q", code)
	}
	if !strings.Contains(code, `fmt.Println("hello")`) {
		t.Fatalf("expected extracted code to contain body, got: %q", code)
	}
}


func TestExtractReviewedGoSnippetCode_MissingCode(t *testing.T) {
	if code := extractReviewedGoSnippetCode(`{"TimeoutSeconds":10}`); code != "" {
		t.Fatalf("expected empty code, got: %q", code)
	}
}
