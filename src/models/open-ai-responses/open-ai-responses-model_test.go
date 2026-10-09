package open_ai_responses

import "testing"

func TestResponseModelVersionCodexUsesGPT61Sol(t *testing.T) {
	for _, requested := range []string{"codex", "gpt-6.1-sol"} {
		if got := responseModelVersion(requested); got != "gpt-6.1-sol" {
			t.Fatalf("expected %q to resolve to gpt-6.1-sol, got %q", requested, got)
		}
	}
}
