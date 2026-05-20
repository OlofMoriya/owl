package tools

import (
	"strings"
	"testing"
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
