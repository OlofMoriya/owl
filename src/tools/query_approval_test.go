package tools

import (
	"strings"
	"testing"
)

func TestParseApprovalDecision(t *testing.T) {
	t.Parallel()

	approved := parseApprovalDecision("approved\n")
	if approved.Result != Approved {
		t.Fatalf("expected approved, got %+v", approved)
	}
	if approved.Reason != "" {
		t.Fatalf("expected empty reason for approval, got %q", approved.Reason)
	}

	rejected := parseApprovalDecision("rejected\nneeds more safety review\n")
	if rejected.Result != Rejected {
		t.Fatalf("expected rejected, got %+v", rejected)
	}
	if rejected.Reason != "needs more safety review" {
		t.Fatalf("unexpected rejection reason: %q", rejected.Reason)
	}

	cancelled := parseApprovalDecision("cancelled\n")
	if cancelled.Result != Cancelled {
		t.Fatalf("expected cancelled, got %+v", cancelled)
	}
}

func TestShowQueryApprovalTerminalReasonParsingHelper(t *testing.T) {
	t.Parallel()

	decision := parseApprovalDecision("rejected\nline one\nline two\n")
	if decision.Result != Rejected {
		t.Fatalf("expected rejected, got %+v", decision)
	}
	if !strings.Contains(decision.Reason, "line one") || !strings.Contains(decision.Reason, "line two") {
		t.Fatalf("expected multiline reason, got %q", decision.Reason)
	}
}
