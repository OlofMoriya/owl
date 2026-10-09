package claude_model

import (
	"encoding/json"
	"testing"

	commontypes "owl/common_types"
	"owl/data"
)

func TestClaudeThinkingPayload(t *testing.T) {
	for _, model := range []string{"claude-sonnet-5-5", "claude-opus-5-5", "claude-sonnet-4-6", "claude-opus-4-6", "claude-haiku-4-5-20251001", "claude-sonnet-4-5-20250929"} {
		for _, thinking := range []bool{false, true} {
			t.Run(model+map[bool]string{false: "/off", true: "/on"}[thinking], func(t *testing.T) {
				payload := createClaudePayload("hello", false, nil, model, thinking, &data.Context{}, &commontypes.PayloadModifiers{})
				encoded, err := json.Marshal(payload)
				if err != nil {
					t.Fatal(err)
				}
				var body map[string]interface{}
				if err := json.Unmarshal(encoded, &body); err != nil {
					t.Fatal(err)
				}
				adaptive := model != "claude-haiku-4-5-20251001" && model != "claude-sonnet-4-5-20250929"
				if !thinking {
					if _, ok := body["thinking"]; ok {
						t.Fatal("thinking should be omitted")
					}
					if _, ok := body["output_config"]; ok {
						t.Fatal("output_config should be omitted")
					}
				} else {
					block := body["thinking"].(map[string]interface{})
					if adaptive {
						if block["type"] != "adaptive" {
							t.Fatalf("unexpected thinking: %v", block)
						}
						if _, ok := block["budget_tokens"]; ok {
							t.Fatal("adaptive thinking must omit budget_tokens")
						}
						if body["output_config"].(map[string]interface{})["effort"] != "medium" {
							t.Fatal("missing effort")
						}
					} else {
						if block["type"] != "enabled" || block["budget_tokens"] != float64(2000) {
							t.Fatalf("unexpected legacy thinking: %v", block)
						}
						if body["temperature"] != float64(1) {
							t.Fatal("legacy thinking requires temperature 1")
						}
					}
				}
				if adaptive {
					if _, ok := body["temperature"]; ok {
						t.Fatal("adaptive models must omit temperature")
					}
				}
			})
		}
	}
}
