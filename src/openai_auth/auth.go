package openai_auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	callback "owl/http/callback"
	"owl/logger"
	"path/filepath"
	"strings"
	"time"
)

const (
	openAIOAuthClientID   = "app_EMoamEEZ73f0CkXaXp7hrann"
	openAIOAuthIssuer     = "https://auth.openai.com"
	openAIOAuthTokenURL   = "https://auth.openai.com/oauth/token"
	openAIOAuthAuthorize  = "https://auth.openai.com/oauth/authorize"
	openAIOAuthEarlySkew  = 60 * 1000
	openAIOAuthFilePath   = ".owl/auth/openai.json"
	openCodeAuthFilePath  = ".local/share/opencode/auth.json"
	openAIOAuthScope      = "openid profile email offline_access"
	openAIOAuthOriginator = "owl"
	oauthCallbackHost     = "127.0.0.1"
	oauthCallbackPort     = 1455
	oauthCallbackPath     = "/auth/callback"
	openAIOAuthFilePerm   = 0o600
	openAIOAuthFolderPerm = 0o700
)

type ResolvedAuth struct {
	Token     string
	AccountID string
	IsCodex   bool
}

type Status string

const (
	StatusNone   Status = "none"
	StatusAPIKey Status = "api_key"
	StatusOAuth  Status = "oauth"
)

type oauthFile struct {
	Type         string `json:"type"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
	AccountID    string `json:"account_id"`
	AccountIDAlt string `json:"accountId,omitempty"`
}

type openCodeAuthFile struct {
	OpenAI struct {
		Type      string `json:"type"`
		Access    string `json:"access"`
		Refresh   string `json:"refresh"`
		Expires   int64  `json:"expires"`
		AccountID string `json:"accountId"`
	} `json:"openai"`
}

type refreshResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

type loginResult struct {
	VerificationURL string
	UserCode        string
}

type LoginSession struct {
	state    string
	verifier string
	callback *callback.OAuthCallbackServer
}

var refreshToken = refreshTokenFromAPI

func Login() (string, error) {
	result, session, err := StartLogin()
	if err != nil {
		return "", err
	}

	return CompleteLogin(result, session)
}

func StartLogin() (loginResult, *LoginSession, error) {
	return startPKCEFlow()
}

func CompleteLogin(result loginResult, session *LoginSession) (string, error) {
	if session == nil {
		return "", fmt.Errorf("login session missing")
	}
	defer session.callback.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	code, err := session.callback.WaitForCode(ctx)
	if err != nil {
		return "", fmt.Errorf("oauth callback failed: %w", err)
	}

	tokens, err := exchangeAuthorizationCode(code, session.verifier, callbackURL())
	if err != nil {
		return "", err
	}

	auth := oauthFile{
		Type:         "oauth",
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		AccountID:    extractAccountID(tokens.AccessToken),
	}
	auth.updateFromRefresh(tokens)

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(home, openAIOAuthFilePath)
	if err := writeOAuthFile(path, auth); err != nil {
		return "", err
	}

	return fmt.Sprintf("OpenAI login successful. Visit %s", result.VerificationURL), nil
}

func Logout() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, openAIOAuthFilePath)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func Resolve() (ResolvedAuth, error) {
	debugln("openai auth resolve: start")
	oauth, path, err := loadOAuthFile()
	if err == nil {
		debugf(
			"openai auth resolve: loaded oauth file path=%s type=%s has_access=%t has_refresh=%t expires_at_ms=%d account_id_present=%t",
			path,
			oauth.Type,
			oauth.AccessToken != "",
			oauth.RefreshToken != "",
			oauth.expiresAt(),
			oauth.resolvedAccountID() != "",
		)
		debugf("openai auth resolve: token fingerprint=%s", tokenFingerprint(oauth.AccessToken))
		now := time.Now().UnixMilli()
		expiresAt := oauth.expiresAt()
		refreshNeeded := oauth.RefreshToken != "" && oauth.AccessToken != "" && now+openAIOAuthEarlySkew >= expiresAt
		debugf(
			"openai auth resolve: refresh decision now_ms=%d expires_at_ms=%d skew_ms=%d refresh_needed=%t",
			now,
			expiresAt,
			openAIOAuthEarlySkew,
			refreshNeeded,
		)
		if refreshNeeded {
			oldFingerprint := tokenFingerprint(oauth.AccessToken)
			oldExpiresAt := oauth.expiresAt()
			debugf("openai auth resolve: refreshing token path=%s", path)
			refreshed, refreshErr := refreshToken(oauth.RefreshToken)
			if refreshErr != nil {
				debugf("openai auth resolve: refresh failed path=%s err=%v", path, refreshErr)
				return ResolvedAuth{}, fmt.Errorf("openai oauth refresh failed: %w", refreshErr)
			}
			oauth.updateFromRefresh(refreshed)
			debugf(
				"openai auth resolve: refresh success token_changed=%t expires_at_old=%d expires_at_new=%d new_fingerprint=%s",
				oldFingerprint != tokenFingerprint(oauth.AccessToken),
				oldExpiresAt,
				oauth.expiresAt(),
				tokenFingerprint(oauth.AccessToken),
			)
			if writeErr := writeOAuthFile(path, oauth); writeErr != nil {
				debugf("openai auth resolve: persisted refresh failed path=%s err=%v", path, writeErr)
				return ResolvedAuth{}, fmt.Errorf("openai oauth persist failed: %w", writeErr)
			}
			debugf("openai auth resolve: persisted refreshed token path=%s", path)
		}

		if oauth.AccessToken != "" {
			accountID := oauth.resolvedAccountID()
			debugf(
				"openai auth resolve: selected_source=oauth source_path=%s selection_reason=oauth_file_present account_id_present=%t token_fingerprint=%s",
				path,
				strings.TrimSpace(accountID) != "",
				tokenFingerprint(oauth.AccessToken),
			)
			return ResolvedAuth{Token: oauth.AccessToken, AccountID: accountID, IsCodex: true}, nil
		}

		debugf("openai auth resolve: oauth file path=%s had empty access token fallback_trigger=empty_access_token", path)
	} else {
		debugf("openai auth resolve: oauth file load failed err=%v fallback_trigger=oauth_file_unavailable", err)
	}

	apiKey, ok := os.LookupEnv("OPENAI_API_KEY")
	if !ok || apiKey == "" {
		debugln("openai auth resolve: no oauth and OPENAI_API_KEY missing")
		return ResolvedAuth{}, fmt.Errorf("could not fetch OPENAI_API_KEY and no codex oauth token found")
	}

	debugf(
		"openai auth resolve: selected_source=api_key selection_reason=oauth_unavailable_or_invalid token_fingerprint=%s",
		tokenFingerprint(apiKey),
	)
	return ResolvedAuth{Token: apiKey, IsCodex: false}, nil
}

func HasCodexOAuthCredential() bool {
	oauth, _, err := loadOAuthFile()
	if err != nil {
		return false
	}
	return oauth.AccessToken != "" && oauth.RefreshToken != ""
}

func CurrentStatus() Status {
	if HasCodexOAuthCredential() {
		return StatusOAuth
	}
	apiKey, ok := os.LookupEnv("OPENAI_API_KEY")
	if ok && apiKey != "" {
		return StatusAPIKey
	}
	return StatusNone
}

func loadOAuthFile() (oauthFile, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return oauthFile{}, "", err
	}

	openCodePath := filepath.Join(home, openCodeAuthFilePath)
	debugf("openai auth resolve: checking opencode auth path=%s", openCodePath)
	if auth, err := loadOpenCodeOAuthFile(openCodePath); err == nil {
		debugf("openai auth resolve: using opencode auth file path=%s", openCodePath)
		return auth, openCodePath, nil
	} else {
		debugf("openai auth resolve: opencode auth file unavailable path=%s err=%v", openCodePath, err)
	}

	path := filepath.Join(home, openAIOAuthFilePath)
	debugf("openai auth resolve: checking owl auth path=%s", path)
	content, err := os.ReadFile(path)
	if err != nil {
		return oauthFile{}, path, err
	}

	var auth oauthFile
	if err := json.Unmarshal(content, &auth); err != nil {
		return oauthFile{}, path, err
	}

	if auth.AccountID == "" && auth.AccountIDAlt != "" {
		auth.AccountID = auth.AccountIDAlt
	}

	return auth, path, nil
}

func loadOpenCodeOAuthFile(path string) (oauthFile, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return oauthFile{}, err
	}

	var authFile openCodeAuthFile
	if err := json.Unmarshal(content, &authFile); err != nil {
		return oauthFile{}, err
	}

	if strings.TrimSpace(authFile.OpenAI.Type) != "oauth" || strings.TrimSpace(authFile.OpenAI.Access) == "" {
		return oauthFile{}, fmt.Errorf("opencode auth missing oauth access token")
	}

	return oauthFile{
		Type:         authFile.OpenAI.Type,
		AccessToken:  authFile.OpenAI.Access,
		RefreshToken: authFile.OpenAI.Refresh,
		ExpiresAt:    authFile.OpenAI.Expires,
		AccountID:    authFile.OpenAI.AccountID,
	}, nil
}

func writeOAuthFile(path string, auth oauthFile) error {
	if err := os.MkdirAll(filepath.Dir(path), openAIOAuthFolderPerm); err != nil {
		return err
	}

	data, err := json.MarshalIndent(auth, "", "  ")
	if err != nil {
		return err
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, openAIOAuthFilePerm); err != nil {
		return err
	}

	return os.Rename(tmp, path)
}

func refreshTokenFromAPI(refreshTok string) (refreshResponse, error) {
	body := map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": refreshTok,
		"client_id":     openAIOAuthClientID,
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return refreshResponse{}, err
	}

	req, err := http.NewRequest(http.MethodPost, openAIOAuthTokenURL, bytes.NewBuffer(encoded))
	if err != nil {
		return refreshResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return refreshResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return refreshResponse{}, fmt.Errorf("oauth refresh failed with status %d", resp.StatusCode)
	}

	var out refreshResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return refreshResponse{}, err
	}
	if out.AccessToken == "" {
		return refreshResponse{}, fmt.Errorf("oauth refresh response missing access_token")
	}

	return out, nil
}

func startPKCEFlow() (loginResult, *LoginSession, error) {
	state, err := randomHex(16)
	if err != nil {
		return loginResult{}, nil, err
	}
	verifier, err := randomBase64URL(32)
	if err != nil {
		return loginResult{}, nil, err
	}

	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	callbackServer, err := callback.StartOAuthCallbackServer(oauthCallbackHost, oauthCallbackPort, state)
	if err != nil {
		return loginResult{}, nil, fmt.Errorf("oauth callback server start failed: %w", err)
	}

	u, _ := url.Parse(openAIOAuthAuthorize)
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", openAIOAuthClientID)
	q.Set("redirect_uri", callbackURL())
	q.Set("scope", openAIOAuthScope)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	q.Set("id_token_add_organizations", "true")
	q.Set("codex_cli_simplified_flow", "true")
	q.Set("originator", openAIOAuthOriginator)
	u.RawQuery = q.Encode()

	return loginResult{VerificationURL: u.String()}, &LoginSession{state: state, verifier: verifier, callback: callbackServer}, nil
}

func exchangeAuthorizationCode(code string, verifier string, redirectURI string) (refreshResponse, error) {
	body := url.Values{}
	body.Set("grant_type", "authorization_code")
	body.Set("code", code)
	body.Set("redirect_uri", redirectURI)
	body.Set("client_id", openAIOAuthClientID)
	body.Set("code_verifier", verifier)

	req, err := http.NewRequest(http.MethodPost, openAIOAuthTokenURL, strings.NewReader(body.Encode()))
	if err != nil {
		return refreshResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return refreshResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return refreshResponse{}, fmt.Errorf("token exchange failed with status %d", resp.StatusCode)
	}

	var out refreshResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return refreshResponse{}, err
	}
	if out.AccessToken == "" || out.RefreshToken == "" {
		return refreshResponse{}, fmt.Errorf("token exchange response missing token fields")
	}
	return out, nil
}

func callbackURL() string {
	return fmt.Sprintf("http://localhost:%d%s", oauthCallbackPort, oauthCallbackPath)
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func randomBase64URL(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func extractAccountID(accessToken string) string {
	parts := bytes.Split([]byte(accessToken), []byte("."))
	if len(parts) != 3 {
		return ""
	}
	payload, err := decodeBase64URL(parts[1])
	if err != nil {
		return ""
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	if value, ok := claims["chatgpt_account_id"].(string); ok {
		return value
	}
	if nested, ok := claims["https://api.openai.com/auth"].(map[string]any); ok {
		if value, ok := nested["chatgpt_account_id"].(string); ok {
			return value
		}
	}
	return ""
}

func decodeBase64URL(raw []byte) ([]byte, error) {
	for len(raw)%4 != 0 {
		raw = append(raw, '=')
	}
	normalized := strings.NewReplacer("-", "+", "_", "/").Replace(string(raw))
	out := make([]byte, len(normalized))
	n, err := base64.StdEncoding.Decode(out, []byte(normalized))
	if err != nil {
		return nil, err
	}
	return out[:n], nil
}

func (a oauthFile) expiresAt() int64 {
	return a.ExpiresAt
}

func (a oauthFile) resolvedAccountID() string {
	if strings.TrimSpace(a.AccountID) != "" {
		return strings.TrimSpace(a.AccountID)
	}
	return strings.TrimSpace(a.AccountIDAlt)
}

func tokenFingerprint(token string) string {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return "none"
	}
	hash := sha256.Sum256([]byte(trimmed))
	return hex.EncodeToString(hash[:6])
}

func debugln(msg string) {
	if logger.Debug != nil {
		logger.Debug.Println(msg)
	}
}

func debugf(format string, args ...interface{}) {
	if logger.Debug != nil {
		logger.Debug.Printf(format, args...)
	}
}

func (a *oauthFile) updateFromRefresh(refresh refreshResponse) {
	a.AccessToken = refresh.AccessToken
	if refresh.RefreshToken != "" {
		a.RefreshToken = refresh.RefreshToken
	}
	expires := time.Now().UnixMilli() + refresh.ExpiresIn*1000
	if refresh.ExpiresIn <= 0 {
		expires = time.Now().UnixMilli() + 3600*1000
	}
	a.ExpiresAt = expires
}
