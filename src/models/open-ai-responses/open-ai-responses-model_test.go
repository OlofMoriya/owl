package open_ai_responses

import "testing"

func TestResponseModelVersionCodexUsesGPT56Sol(t *testing.T) {
	for _, requested := range []string{"codex", "gpt-5.6-sol"} {
		if got := responseModelVersion(requested); got != "gpt-5.6-sol" {
			t.Fatalf("expected %q to resolve to gpt-5.6-sol, got %q", requested, got)
		}
	}
}
