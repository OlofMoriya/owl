package tools

import (
	"io"
	"log"
	"os"
	"owl/interaction"
	"owl/logger"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestValidateUnifiedDiff_Valid(t *testing.T) {
	diff := strings.Join([]string{
		"--- a/file.txt",
		"+++ b/file.txt",
		"@@ -1,1 +1,1 @@",
		"-old line",
		"+new line",
		"",
	}, "\n")

	if err := validateUnifiedDiff(diff); err != nil {
		t.Fatalf("expected valid diff, got error: %v", err)
	}
}

func TestValidateUnifiedDiff_MissingPrefixInHunkBody(t *testing.T) {
	diff := strings.Join([]string{
		"--- a/file.txt",
		"+++ b/file.txt",
		"@@ -1,1 +1,1 @@",
		"func brokenLine() {}",
		"",
	}, "\n")

	err := validateUnifiedDiff(diff)
	if err == nil {
		t.Fatalf("expected validation error for malformed hunk body")
	}

	if !strings.Contains(err.Error(), "malformed hunk body") {
		t.Fatalf("expected malformed hunk body error, got: %v", err)
	}
}

func TestValidateUnifiedDiff_HunkCountMismatchAllowed(t *testing.T) {
	diff := strings.Join([]string{
		"--- a/file.txt",
		"+++ b/file.txt",
		"@@ -1,1 +1,2 @@",
		"-old line",
		"+new line",
		"",
	}, "\n")

	if err := validateUnifiedDiff(diff); err != nil {
		t.Fatalf("expected hunk count mismatch to be accepted, got: %v", err)
	}
}

func TestValidateUnifiedDiff_EmptyFirstHunkThenAnotherHunkAllowed(t *testing.T) {
	diff := strings.Join([]string{
		"--- a/file.txt",
		"+++ b/file.txt",
		"@@ -5,6 +5,7 @@ import (",
		"@@ -10,1 +10,1 @@",
		"-old line",
		"+new line",
		"",
	}, "\n")

	if err := validateUnifiedDiff(diff); err != nil {
		t.Fatalf("expected validator to allow empty first hunk, got: %v", err)
	}
}

func TestValidateUnifiedDiff_BareAtAtHeaderAllowed(t *testing.T) {
	diff := strings.Join([]string{
		"--- a/file.txt",
		"+++ b/file.txt",
		"@@",
		"-old line",
		"+new line",
		"",
	}, "\n")

	if err := validateUnifiedDiff(diff); err != nil {
		t.Fatalf("expected bare @@ header to be accepted, got: %v", err)
	}
}

func TestApplyDiffRecountsIncorrectModelHunkCounts(t *testing.T) {
	withWorkingDirectory(t, func(dir string) {
		if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("one\ntwo\nthree\nfour\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		diff := strings.Join([]string{
			"--- a/file.txt",
			"+++ b/file.txt",
			"@@ -1,9 +1,7 @@",
			" one",
			"-two",
			"-three",
			" four",
			"",
		}, "\n")

		if _, err := (&FileUpdateTool{}).applyDiff("file.txt", diff); err != nil {
			t.Fatalf("expected incorrect hunk counts to be recounted: %v", err)
		}

		got, err := os.ReadFile("file.txt")
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "one\nfour\n" {
			t.Fatalf("unexpected file content after apply: %q", got)
		}
	})
}

func TestApplyDiffFailureDoesNotPartiallyApplyEarlierHunks(t *testing.T) {
	withWorkingDirectory(t, func(dir string) {
		original := "one\ntwo\nthree\nfour\n"
		if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte(original), 0o644); err != nil {
			t.Fatal(err)
		}

		diff := strings.Join([]string{
			"--- a/file.txt",
			"+++ b/file.txt",
			"@@ -1,2 +1,2 @@",
			" one",
			"-two",
			"+TWO",
			"@@ -3,2 +3,2 @@",
			" missing model context",
			"-four",
			"+FOUR",
			"",
		}, "\n")

		t.Setenv("HOME", dir)
		if _, err := (&FileUpdateTool{}).applyDiff("file.txt", diff); err == nil {
			t.Fatal("expected patch with invented context to fail")
		}

		got, err := os.ReadFile("file.txt")
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != original {
			t.Fatalf("failed patch partially modified file: %q", got)
		}
	})
}

func withWorkingDirectory(t *testing.T, fn func(string)) {
	t.Helper()
	oldLogger := logger.Debug
	logger.Debug = log.New(io.Discard, "", 0)
	defer func() { logger.Debug = oldLogger }()

	dir := t.TempDir()
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	}()
	fn(dir)
}

func TestIntegratedDiffApprovalRoutesDecisionThroughTUI(t *testing.T) {
	original := interaction.DiffApprovalPromptChan
	interaction.DiffApprovalPromptChan = make(chan interaction.DiffApprovalPrompt, 1)
	defer func() { interaction.DiffApprovalPromptChan = original }()

	type approvalOutcome struct {
		decision ApprovalDecision
		err      error
	}
	outcomeChan := make(chan approvalOutcome, 1)
	go func() {
		decision, err := requestIntegratedDiffApproval("src/example.go", "diff", "Explain the change")
		outcomeChan <- approvalOutcome{decision: decision, err: err}
	}()

	var prompt interaction.DiffApprovalPrompt
	select {
	case prompt = <-interaction.DiffApprovalPromptChan:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for integrated diff approval prompt")
	}

	if prompt.FileName != "src/example.go" || prompt.Diff != "diff" || prompt.Summary != "Explain the change" {
		t.Fatalf("unexpected approval prompt: %+v", prompt)
	}
	prompt.ResponseChan <- interaction.DiffApprovalResult{
		Decision: interaction.DiffApprovalRejected,
		Reason:   "needs a test",
	}

	select {
	case outcome := <-outcomeChan:
		if outcome.err != nil {
			t.Fatalf("approval failed: %v", outcome.err)
		}
		if outcome.decision.Result != Rejected || outcome.decision.Reason != "needs a test" {
			t.Fatalf("unexpected mapped decision: %+v", outcome.decision)
		}
	case <-time.After(time.Second):
		t.Fatal("integrated approval did not unblock after the TUI response")
	}
}

func TestUpdateFileDefinitionRequiresReviewSummary(t *testing.T) {
	definition, _ := (&FileUpdateTool{}).GetDefinition()
	if !slices.Contains(definition.InputSchema.Required, "Summary") {
		t.Fatalf("expected Summary to be required, got %v", definition.InputSchema.Required)
	}
}
