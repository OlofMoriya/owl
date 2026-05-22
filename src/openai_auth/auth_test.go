package openai_auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResolveFallsBackToAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "api-key-123")
	t.Setenv("HOME", t.TempDir())

	auth, err := Resolve()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if auth.Token != "api-key-123" {
		t.Fatalf("expected api key token, got %q", auth.Token)
	}
	if auth.IsCodex {
		t.Fatalf("expected non-codex auth")
	}
}

func TestResolveUsesPersistedOAuth(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "api-key-123")
	home := t.TempDir()
	t.Setenv("HOME", home)

	path := filepath.Join(home, openAIOAuthFilePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	file := oauthFile{
		Type:         "oauth",
		AccessToken:  "oauth-access",
		RefreshToken: "oauth-refresh",
		ExpiresAt:    time.Now().Add(10 * time.Minute).UnixMilli(),
		AccountID:    "acc-1",
	}
	encoded, _ := json.Marshal(file)
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	auth, err := Resolve()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if auth.Token != "oauth-access" {
		t.Fatalf("expected oauth token, got %q", auth.Token)
	}
	if auth.AccountID != "acc-1" {
		t.Fatalf("expected account id, got %q", auth.AccountID)
	}
	if !auth.IsCodex {
		t.Fatalf("expected codex auth mode")
	}
}

func TestResolvePrefersOpenCodeAuthFile(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	home := t.TempDir()
	t.Setenv("HOME", home)

	opencodePath := filepath.Join(home, openCodeAuthFilePath)
	if err := os.MkdirAll(filepath.Dir(opencodePath), 0o700); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	opencodeContent := []byte(`{"openai":{"type":"oauth","access":"opencode-access","refresh":"opencode-refresh","expires":9999999999999,"accountId":"opencode-acc"}}`)
	if err := os.WriteFile(opencodePath, opencodeContent, 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	owlPath := filepath.Join(home, openAIOAuthFilePath)
	if err := os.MkdirAll(filepath.Dir(owlPath), 0o700); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	owlContent := []byte(`{"type":"oauth","access_token":"owl-access","refresh_token":"owl-refresh","expires_at":9999999999999,"account_id":"owl-acc"}`)
	if err := os.WriteFile(owlPath, owlContent, 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	auth, err := Resolve()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if auth.Token != "opencode-access" {
		t.Fatalf("expected opencode token, got %q", auth.Token)
	}
	if auth.AccountID != "opencode-acc" {
		t.Fatalf("expected opencode account id, got %q", auth.AccountID)
	}
}

func TestResolveReadsCamelCaseAccountIdFromOwlAuthFile(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	home := t.TempDir()
	t.Setenv("HOME", home)

	path := filepath.Join(home, openAIOAuthFilePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	content := []byte(`{"type":"oauth","access_token":"oauth-access","refresh_token":"oauth-refresh","expires_at":9999999999999,"accountId":"acc-camel"}`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	auth, err := Resolve()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if auth.AccountID != "acc-camel" {
		t.Fatalf("expected camel account id, got %q", auth.AccountID)
	}
}

func TestResolveRefreshesExpiredToken(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	home := t.TempDir()
	t.Setenv("HOME", home)

	path := filepath.Join(home, openAIOAuthFilePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	file := oauthFile{
		Type:         "oauth",
		AccessToken:  "old-access",
		RefreshToken: "oauth-refresh",
		ExpiresAt:    time.Now().Add(-time.Minute).UnixMilli(),
		AccountID:    "acc-1",
	}
	encoded, _ := json.Marshal(file)
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	original := refreshToken
	t.Cleanup(func() { refreshToken = original })
	refreshToken = func(token string) (refreshResponse, error) {
		return refreshResponse{AccessToken: "new-access", RefreshToken: "new-refresh", ExpiresIn: 3600}, nil
	}

	auth, err := Resolve()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if auth.Token != "new-access" {
		t.Fatalf("expected refreshed token, got %q", auth.Token)
	}

	updatedRaw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	var updated oauthFile
	if err := json.Unmarshal(updatedRaw, &updated); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if updated.AccessToken != "new-access" {
		t.Fatalf("expected persisted refreshed token, got %q", updated.AccessToken)
	}
}

func TestCurrentStatus(t *testing.T) {
	t.Run("oauth", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("OPENAI_API_KEY", "")

		path := filepath.Join(home, openAIOAuthFilePath)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}
		content := []byte(`{"type":"oauth","access_token":"token","refresh_token":"refresh","expires_at":9999999999999}`)
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatalf("write failed: %v", err)
		}

		if got := CurrentStatus(); got != StatusOAuth {
			t.Fatalf("expected oauth status, got %q", got)
		}
	})

	t.Run("api_key", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("OPENAI_API_KEY", "api-key-123")
		if got := CurrentStatus(); got != StatusAPIKey {
			t.Fatalf("expected api key status, got %q", got)
		}
	})

	t.Run("none", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("OPENAI_API_KEY", "")
		if got := CurrentStatus(); got != StatusNone {
			t.Fatalf("expected none status, got %q", got)
		}
	})
}

func TestLogoutRemovesAuthFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path := filepath.Join(home, openAIOAuthFilePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"type":"oauth"}`), 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if err := Logout(); err != nil {
		t.Fatalf("logout failed: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected auth file removed, got err=%v", err)
	}
}
