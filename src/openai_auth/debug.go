package openai_auth

import (
	"bytes"
	"encoding/json"
	"owl/logger"
)

// DebugToken logs detailed information about the JWT token for debugging
func DebugToken(token string) {
	logger.Debug.Println("=== TOKEN DEBUG ===")

	parts := bytes.Split([]byte(token), []byte("."))
	if len(parts) != 3 {
		logger.Debug.Println("Invalid JWT format - expected 3 parts")
		return
	}

	payload, err := decodeBase64URL(parts[1])
	if err != nil {
		logger.Debug.Printf("Failed to decode JWT payload: %v", err)
		return
	}

	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		logger.Debug.Printf("Failed to unmarshal JWT claims: %v", err)
		return
	}

	pretty, _ := json.MarshalIndent(claims, "", "  ")
	logger.Debug.Printf("Token Claims:\n%s", string(pretty))

	// Specifically check for scope
	if scope, ok := claims["scope"].(string); ok {
		logger.Debug.Printf("✓ Scopes found: %s", scope)
	} else {
		logger.Debug.Println("✗ No 'scope' claim found in token")
	}

	// Check account ID extraction
	accountID := extractAccountID(token)
	logger.Debug.Printf("Extracted Account ID: %s", accountID)
	logger.Debug.Println("===================")
}
