package models

import (
	"os"
	"path/filepath"
	"testing"

	open_ai_gpt_model "owl/models/open-ai-gpt"
	open_ai_responses "owl/models/open-ai-responses"
)

func TestGetModelForQuery_DefaultsToClaudeWithoutCodexAuth(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	_, modelName := GetModelForQuery("", nil, nil, nil, false, false, false, false)
	if modelName != "claude" {
		t.Fatalf("expected default model claude, got %q", modelName)
	}
}

func TestGetModelForQuery_GptUsesChatCompletionWithoutCodexAuth(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	model, _ := GetModelForQuery("gpt", nil, nil, nil, false, false, false, false)
	if _, ok := model.(*open_ai_gpt_model.OpenAIGPTModel); !ok {
		t.Fatalf("expected chat-completions GPT model without oauth")
	}
}

func TestGetModelForQuery_DefaultsToCodexWithCodexAuth(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path := filepath.Join(home, ".owl", "auth", "openai.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	content := []byte(`{"type":"oauth","access_token":"token","refresh_token":"refresh","expires_at":9999999999999}`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	_, modelName := GetModelForQuery("", nil, nil, nil, false, false, false, false)
	if modelName != "codex" {
		t.Fatalf("expected default model codex, got %q", modelName)
	}
}

func TestGetModelForQuery_GptUsesResponsesWithCodexAuth(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path := filepath.Join(home, ".owl", "auth", "openai.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	content := []byte(`{"type":"oauth","access_token":"token","refresh_token":"refresh","expires_at":9999999999999}`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	model, _ := GetModelForQuery("gpt", nil, nil, nil, false, false, false, false)
	if _, ok := model.(*open_ai_responses.OpenAiResponseModel); !ok {
		t.Fatalf("expected responses model with oauth")
	}
}
