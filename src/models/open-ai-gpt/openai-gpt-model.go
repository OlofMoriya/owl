package openai_gpt_model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	neturl "net/url"
	"os"
	commontypes "owl/common_types"
	"owl/data"
	"owl/logger"
	"owl/models/open-ai-base"
	"owl/openai_auth"
	"strings"
)

type OpenAIGPTModel struct {
	openai_base.OpenAICompatibleModel
	ModelVersion string
}

func (model *OpenAIGPTModel) SetResponseHandler(responseHandler commontypes.ResponseHandler) {
	model.ResponseHandler = responseHandler
}

func (model *OpenAIGPTModel) CreateRequest(context *data.Context, prompt string, streaming bool, history []data.History, modifiers *commontypes.PayloadModifiers) *http.Request {
	// Initialize the base model fields
	model.Prompt = prompt
	model.AccumulatedAnswer = ""
	model.ContextId = context.Id
	model.Context = context
	model.StreamedToolCalls = make(map[int]*openai_base.StreamingToolCall)
	model.Modifiers = modifiers

	var model_version string
	switch model.ModelVersion {
	case "codex":
		model_version = "gpt-5.3-codex"
	case "gpt":
		model_version = "gpt-5.3-chat-latest"
	case "gpt-5.4":
		model_version = "gpt-5.4"
	case "gpt-5.5":
		model_version = "gpt-5.5"
	case "gpt-mini":
		model_version = "gpt-5.4-mini-2026-03-17"
	case "gpt-nano":
		model_version = "gpt-5.4-nano-2026-03-17"
	default:
		model_version = "gpt-5.4"
	}
	model.ModelName = model_version

	// Check if web search is enabled - use different API endpoint
	if modifiers.Web {
		logger.Debug.Println("Web search enabled, using /v1/responses endpoint")
		payload := openai_base.CreateWebSearchPayload(prompt, history, model_version, context)
		return createOpenAIGPTRequest(payload, true)
	}

	// Standard chat completions request
	payload := openai_base.CreatePayload(prompt, streaming, history, modifiers, model_version, 16000, context)
	return createOpenAIGPTRequest(payload, false)
}

func (model *OpenAIGPTModel) HandleStreamedLine(line []byte) {
	// Delegate to base implementation
	model.OpenAICompatibleModel.HandleStreamedLine(line, model)
}

func (model *OpenAIGPTModel) HandleBodyBytes(bytes []byte) {
	// Try to detect if this is a web search response
	var webSearchResponse openai_base.ResponseAPIResponse
	if err := json.Unmarshal(bytes, &webSearchResponse); err == nil && len(webSearchResponse.Output) > 0 {
		logger.Debug.Println("Detected OpenAI web search response format (output array present)")
		model.OpenAICompatibleModel.HandleWebSearchResponse(bytes, model)
		return
	}

	// Otherwise delegate to standard base implementation
	model.OpenAICompatibleModel.HandleBodyBytes(bytes, model)
}

func createOpenAIGPTRequest(payload interface{}, isWebSearch bool) *http.Request {
	auth, err := openai_auth.Resolve()
	if err != nil {
		apiKey, ok := os.LookupEnv("OPENAI_API_KEY")
		if !ok || strings.TrimSpace(apiKey) == "" {
			panic(fmt.Errorf("could not resolve openai auth: %w", err))
		}
		logger.Debug.Printf("openai chat auth resolve failed, falling back to OPENAI_API_KEY: %v", err)
		auth = openai_auth.ResolvedAuth{Token: apiKey, IsCodex: false}
	}
	authSource := "api_key"
	if auth.IsCodex {
		authSource = "oauth"
	}
	logger.Debug.Printf("openai chat auth source: %s", authSource)

	jsonpayload, err := json.Marshal(payload)
	if err != nil {
		panic("failed to marshal payload")
	}

	logger.Debug.Printf("OpenAI chat-completions request payload:\n%s", string(jsonpayload))

	// Use different endpoint for web search
	url := "https://api.openai.com/v1/chat/completions"
	selectionReason := "standard_chat_completions"
	if isWebSearch {
		url = "https://api.openai.com/v1/responses"
		selectionReason = "web_search_enabled"
		logger.Debug.Println("Using OpenAI web search endpoint: /v1/responses")
	}
	parsedURL, parseErr := neturl.Parse(url)
	if parseErr != nil {
		logger.Debug.Printf("openai chat endpoint parse failed url=%s err=%v", url, parseErr)
	} else {
		logger.Debug.Printf(
			"openai chat endpoint selected host=%s path=%s reason=%s auth_source=%s",
			parsedURL.Host,
			parsedURL.Path,
			selectionReason,
			authSource,
		)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonpayload))
	if err != nil {
		panic(fmt.Errorf("failed to create request: %v", err))
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", auth.Token))
	req.Header.Set("X-Owl-Auth-Source", authSource)
	if auth.IsCodex && strings.TrimSpace(auth.AccountID) != "" {
		req.Header.Set("ChatGPT-Account-Id", auth.AccountID)
		req.Header.Set("ChatGPT-Account-ID", auth.AccountID)
		req.Header.Set("OpenAI-Account-ID", auth.AccountID)
		logger.Debug.Printf("openai chat account header present: true")
	} else {
		logger.Debug.Printf("openai chat account header present: false")
	}
	logger.Debug.Printf("openai chat request ready endpoint=%s auth_source=%s", url, authSource)

	return req
}
