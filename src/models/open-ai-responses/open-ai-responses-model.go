package open_ai_responses

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/png"
	"net/http"
	"net/url"
	"os"
	commontypes "owl/common_types"
	"owl/data"
	"owl/logger"
	"owl/mode"
	"owl/openai_auth"
	"owl/services"
	"owl/tools"
	"strings"
	"time"

	"github.com/skratchdot/open-golang/open"
)

var MODELNAME = "open_ai_responses"

// StreamedFunctionCall tracks a function call being accumulated during streaming
type StreamedFunctionCall struct {
	ID        string
	CallID    string
	Name      string
	Arguments string
}

type OpenAiResponseModel struct {
	HistoryRepository     data.HistoryRepository
	ResponseHandler       commontypes.ResponseHandler
	prompt                string
	accumulatedAnswer     string
	contextId             int64
	modelName             string
	ModelVersion          string
	Modifiers             *commontypes.PayloadModifiers
	currentFunctionCall   *StreamedFunctionCall
	streamedFunctionCalls []StreamedFunctionCall
	streamedToolUses      []data.ToolUse
}

func (model *OpenAiResponseModel) CreateRequest(context *data.Context, prompt string, streaming bool, history []data.History, modifiers *commontypes.PayloadModifiers) *http.Request {
	payload := createResponsePayload(context, prompt, streaming, history, modifiers, model.ModelVersion)
	model.prompt = prompt
	model.accumulatedAnswer = ""
	model.contextId = context.Id
	model.modelName = payload.Model
	model.Modifiers = modifiers
	model.currentFunctionCall = nil
	model.streamedFunctionCalls = nil
	model.streamedToolUses = nil
	return createRequest(payload)
}

func createRequest(payload RequestPayload) *http.Request {
	auth, err := openai_auth.Resolve()
	if err != nil {
		apiKey, ok := os.LookupEnv("OPENAI_API_KEY")
		if !ok || strings.TrimSpace(apiKey) == "" {
			panic(fmt.Errorf("could not resolve openai auth: %w", err))
		}
		logger.Debug.Printf("openai responses auth resolve failed, falling back to OPENAI_API_KEY: %v", err)
		auth = openai_auth.ResolvedAuth{Token: apiKey, IsCodex: false}
	}
	authSource := "api_key"
	if auth.IsCodex {
		authSource = "oauth"
	}
	logger.Debug.Printf("openai responses auth source: %s", authSource)
	if auth.IsCodex {
		payload = adaptPayloadForCodex(payload)
	}

	jsonpayload, err := json.Marshal(payload)
	logger.Debug.Println("Will send payload")
	logger.Debug.Println(string(jsonpayload))
	if err != nil {
		panic("failed to marshal payload")
	}

	requestURL, selectionReason := resolveResponsesEndpoint(auth)
	parsedURL, parseErr := url.Parse(requestURL)
	if parseErr != nil {
		logger.Debug.Printf("openai responses endpoint parse failed url=%s err=%v", requestURL, parseErr)
	} else {
		logger.Debug.Printf(
			"openai responses endpoint selected host=%s path=%s reason=%s auth_source=%s",
			parsedURL.Host,
			parsedURL.Path,
			selectionReason,
			authSource,
		)
	}

	req, err := http.NewRequest("POST", requestURL, bytes.NewBuffer(jsonpayload))
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
		logger.Debug.Printf("openai responses account header present: true")
	} else {
		logger.Debug.Printf("openai responses account header present: false")
	}

	logger.Debug.Printf("openai responses request ready model=%s endpoint=%s auth_source=%s", payload.Model, requestURL, authSource)

	return req
}

func adaptPayloadForCodex(payload RequestPayload) RequestPayload {
	store := false
	payload.Store = &store
	if payload.Stream == nil {
		stream := true
		payload.Stream = &stream
	}

	inputItems, ok := payload.Input.([]interface{})
	if !ok {
		if payload.Instructions == nil || strings.TrimSpace(*payload.Instructions) == "" {
			defaultInstructions := "You are a helpful assistant."
			payload.Instructions = &defaultInstructions
			logger.Debug.Printf("openai responses codex payload adjustment: input_not_array set_default_instructions=true store=false stream=%t", payload.Stream != nil && *payload.Stream)
		}
		return payload
	}

	systemParts := []string{}
	filtered := make([]interface{}, 0, len(inputItems))
	for _, item := range inputItems {
		msg, ok := item.(InputMessage)
		if ok && strings.EqualFold(msg.Role, "system") {
			trimmed := strings.TrimSpace(msg.Content)
			if trimmed != "" {
				systemParts = append(systemParts, trimmed)
			}
			continue
		}
		filtered = append(filtered, item)
	}

	payload.Input = filtered
	if payload.Instructions == nil || strings.TrimSpace(*payload.Instructions) == "" {
		instructions := strings.TrimSpace(strings.Join(systemParts, "\n\n"))
		if instructions == "" {
			instructions = "You are a helpful assistant."
		}
		payload.Instructions = &instructions
	}

	logger.Debug.Printf(
		"openai responses codex payload adjustment: removed_system_messages=%d instructions_present=%t input_items_after=%d store=false stream=%t",
		len(inputItems)-len(filtered),
		payload.Instructions != nil && strings.TrimSpace(*payload.Instructions) != "",
		len(filtered),
		payload.Stream != nil && *payload.Stream,
	)
	return payload
}

func resolveResponsesEndpoint(auth openai_auth.ResolvedAuth) (string, string) {
	codexURL := strings.TrimSpace(os.Getenv("OWL_OPENAI_CODEX_RESPONSES_URL"))
	if codexURL == "" {
		codexURL = "https://chatgpt.com/backend-api/codex/responses"
	}
	if auth.IsCodex {
		return codexURL, "codex_oauth"
	}
	return "https://api.openai.com/v1/responses", "api_key_or_non_codex"
}

func createResponsePayload(context *data.Context, prompt string, streaming bool, history []data.History, modifiers *commontypes.PayloadModifiers, requestedModel string) RequestPayload {
	modelVersion := "gpt-5.3-chat-latest"
	if requestedModel == "codex" {
		modelVersion = "gpt-5.3-codex"
	} else if requestedModel == "gpt" {
		modelVersion = "gpt-5.3-chat-latest"
	} else if requestedModel == "gpt-5.5" {
		modelVersion = "gpt-5.5"
	} else if requestedModel == "gpt-5.4" {
		modelVersion = "gpt-5.4"
	}

	if modifiers == nil {
		modifiers = &commontypes.PayloadModifiers{}
	}

	disableTools := strings.EqualFold(strings.TrimSpace(os.Getenv("OWL_OPENAI_RESPONSES_DISABLE_TOOLS")), "true")

	toolList := []Tool{}
	if !disableTools && modifiers != nil {
		if modifiers.Image {
			toolList = append(toolList, Tool{Type: "image_generation"})
		}
		if modifiers.Web {
			toolList = append(toolList, Tool{Type: "web_search"})
			toolList = append(toolList, Tool{Type: "web_fetch"})
		}
	}

	if !disableTools {
		customTools := tools.GetCustomTools(mode.Mode, modifiers.ToolGroupFilters...)
		for _, customTool := range customTools {
			params := map[string]interface{}{
				"type":       customTool.InputSchema.Type,
				"properties": convertProperties(customTool.InputSchema.Properties),
			}
			if len(customTool.InputSchema.Required) > 0 {
				params["required"] = customTool.InputSchema.Required
			}
			toolList = append(toolList, Tool{
				Type:        "function",
				Name:        customTool.Name,
				Description: customTool.Description,
				Parameters:  params,
			})
		}
	} else {
		logger.Debug.Println("openai responses tools disabled via OWL_OPENAI_RESPONSES_DISABLE_TOOLS=true")
	}

	input := buildInput(context, prompt, history, modifiers)

	request := RequestPayload{
		Model: modelVersion,
		Input: input,
	}
	if len(toolList) > 0 {
		request.Tools = toolList
	}
	if streaming {
		stream := true
		request.Stream = &stream
	}

	logger.Debug.Println("Will send payload")
	logger.Debug.Printf("request %v", request)
	return request
}

// getFirstNWords returns the first N words from a string
func getFirstNWords(text string, n int) string {
	words := strings.Fields(text)
	if len(words) <= n {
		return text
	}
	return strings.Join(words[:n], " ") + "..."
}

func buildInput(context *data.Context, prompt string, history []data.History, modifiers *commontypes.PayloadModifiers) interface{} {
	logger.Debug.Println("========================================")
	logger.Debug.Println("Building payload input - conversation structure:")
	logger.Debug.Println("========================================")

	if modifiers == nil {
		modifiers = &commontypes.PayloadModifiers{}
	}

	items := []interface{}{}
	replayedToolUseIDs := map[string]bool{}

	if context != nil {
		systemPrompt := strings.TrimSpace(context.SystemPrompt)
		if systemPrompt != "" {
			items = append(items, InputMessage{Role: "system", Content: systemPrompt})
			logger.Debug.Printf("SYSTEM: %s", getFirstNWords(systemPrompt, 5))
		}
	}

	startIdx := 0
	logger.Debug.Printf("Processing %d history entries (from index %d to %d)", len(history)-startIdx, startIdx, len(history)-1)

	// Process history (including tool results)
	for i := startIdx; i < len(history); i++ {
		h := history[i]

		// Add user prompt
		if p := strings.TrimSpace(h.Prompt); p != "" {
			items = append(items, InputMessage{Role: "user", Content: p})
			logger.Debug.Printf("USER: %s", getFirstNWords(p, 5))
		}

		// Check if this history entry has tool uses
		localTools := filterLocalToolUses(h.ToolUse)
		if len(localTools) > 0 {
			logger.Debug.Printf("  (has %d tool uses)", len(localTools))
			// This history entry involved tool calls - render them as function_call + function_call_output items
			for _, tu := range localTools {
				callID := strings.TrimSpace(tu.Id)
				if callID == "" {
					continue
				}

				// Add the function call
				items = append(items, InputFunctionCall{
					Type:      "function_call",
					CallID:    callID,
					Name:      tu.Name,
					Arguments: tu.Input,
				})
				logger.Debug.Printf("  FUNCTION_CALL: %s (id: %s)", tu.Name, callID)

				// Add the function call output
				items = append(items, InputFunctionCallOutput{
					Type:   "function_call_output",
					CallID: callID,
					Output: tu.Result.Content,
				})
				resultPreview := getFirstNWords(tu.Result.Content, 5)
				logger.Debug.Printf("  FUNCTION_OUTPUT: %s", resultPreview)

				// Track that we've replayed this tool use
				replayedToolUseIDs[tu.Id] = true
			}

			// Add assistant's final response if any (after processing tools)
			if r := strings.TrimSpace(h.Response); r != "" {
				items = append(items, InputMessage{Role: "assistant", Content: r})
				logger.Debug.Printf("ASSISTANT: %s", getFirstNWords(r, 5))
			}
		} else {
			// No tool uses - just add the assistant response as text
			if r := strings.TrimSpace(h.Response); r != "" {
				items = append(items, InputMessage{Role: "assistant", Content: r})
				logger.Debug.Printf("ASSISTANT: %s", getFirstNWords(r, 5))
			}
		}
	}

	// Add tool responses from modifiers if this is a continuation (follow-up query)
	// Only add tools that haven't already been replayed from history
	modifierLocalToolUses := filterLocalToolUses(modifiers.ToolUses)
	if len(modifierLocalToolUses) > 0 {
		logger.Debug.Printf("Processing %d tool uses from modifiers", len(modifierLocalToolUses))
		addedToolResponses := 0
		for _, tr := range modifierLocalToolUses {
			if replayedToolUseIDs[tr.Id] {
				logger.Debug.Printf("  Skipping tool %s (id: %s) - already in history", tr.Name, tr.Id)
				continue // Skip - already in history
			}

			callID := strings.TrimSpace(tr.Id)
			if callID == "" {
				continue
			}

			// Add the function call
			items = append(items, InputFunctionCall{
				Type:      "function_call",
				CallID:    callID,
				Name:      tr.Name,
				Arguments: tr.Input,
			})
			logger.Debug.Printf("  FUNCTION_CALL: %s (id: %s)", tr.Name, callID)

			// Add the function call output
			items = append(items, InputFunctionCallOutput{
				Type:   "function_call_output",
				CallID: callID,
				Output: tr.Result.Content,
			})
			resultPreview := getFirstNWords(tr.Result.Content, 5)
			logger.Debug.Printf("  FUNCTION_OUTPUT: %s", resultPreview)

			addedToolResponses++
		}
		if addedToolResponses > 0 {
			logger.Debug.Printf("Added %d tool responses to payload", addedToolResponses)
		}
	}

	// Add current prompt if provided
	if p := strings.TrimSpace(prompt); p != "" {
		items = append(items, InputMessage{Role: "user", Content: p})
		logger.Debug.Printf("USER (current): %s", getFirstNWords(p, 5))
	}

	logger.Debug.Printf("========================================")
	logger.Debug.Printf("Total items in payload: %d", len(items))
	logger.Debug.Println("========================================")

	return items
}

func convertProperties(props map[string]tools.Property) map[string]interface{} {
	result := make(map[string]interface{})
	for key, prop := range props {
		propMap := map[string]interface{}{"type": prop.Type}
		if prop.Description != "" {
			propMap["description"] = prop.Description
		}
		if len(prop.Properties) > 0 {
			propMap["properties"] = convertProperties(prop.Properties)
		}
		if prop.Items != nil {
			propMap["items"] = convertProperty(*prop.Items)
		}
		result[key] = propMap
	}
	return result
}

func convertProperty(prop tools.Property) map[string]interface{} {
	propMap := map[string]interface{}{"type": prop.Type}
	if prop.Description != "" {
		propMap["description"] = prop.Description
	}
	if len(prop.Properties) > 0 {
		propMap["properties"] = convertProperties(prop.Properties)
	}
	if prop.Items != nil {
		propMap["items"] = convertProperty(*prop.Items)
	}
	return propMap
}

func (model *OpenAiResponseModel) HandleStreamedLine(line []byte) {
	responseLine := string(line)

	logger.Debug.Printf("streamed raw line: %s", strings.TrimSpace(responseLine))

	if strings.HasPrefix(responseLine, "data: ") {
		responseData, _ := strings.CutPrefix(responseLine, "data: ")
		responseData = strings.TrimSpace(responseData)
		if responseData == "" || responseData == "[DONE]" {
			logger.Debug.Printf("streamed: empty data or [DONE]")
			return
		}

		var event map[string]interface{}
		if err := json.Unmarshal([]byte(responseData), &event); err != nil {
			logger.Debug.Printf("streamed: JSON unmarshal error: %v (data: %.200s)", err, responseData)
			return
		}

		eventType, _ := event["type"].(string)
		logger.Debug.Printf("streamed event type: %s", eventType)

		switch eventType {
		case "response.output_text.delta", "response.refusal.delta", "response.content_part.added":
			text := extractEventText(event)
			if text != "" {
				model.accumulatedAnswer += text
				model.ResponseHandler.RecievedText(text, nil)
			}

		case "response.reasoning_summary_text.delta":
			text := extractEventText(event)
			if text != "" {
				grey := "grey"
				model.accumulatedAnswer += text
				model.ResponseHandler.RecievedText(text, &grey)
			}

		case "response.output_item.added":
			if item, ok := event["item"].(map[string]interface{}); ok {
				model.handleStreamedOutputItem(item, false)
			}

		case "response.output_item.done":
			if item, ok := event["item"].(map[string]interface{}); ok {
				model.handleStreamedOutputItem(item, true)
			}

		case "response.function_call_arguments.delta":
			if model.currentFunctionCall != nil {
				if delta, ok := event["delta"].(string); ok {
					model.currentFunctionCall.Arguments += delta
				}
			}

		case "response.function_call_arguments.done":
			if model.currentFunctionCall != nil {
				// Use the final arguments if provided
				if args, ok := event["arguments"].(string); ok {
					model.currentFunctionCall.Arguments = args
				}
				model.appendStreamedFunctionCall(*model.currentFunctionCall)
				logger.Debug.Printf("streaming function call done: %s args=%s", model.currentFunctionCall.Name, model.currentFunctionCall.Arguments)
				model.currentFunctionCall = nil
			}

		case "response.output_text.done":
			logger.Debug.Printf("streamed: output_text.done")
			if text := extractEventText(event); text != "" && !strings.Contains(model.accumulatedAnswer, text) {
				model.accumulatedAnswer += text
			}

		case "response.completed":
			logger.Debug.Printf("streamed: response.completed, accumulated %d chars, %d pending function calls", len(model.accumulatedAnswer), len(model.streamedFunctionCalls))

			if text := extractEventText(event); text != "" && !strings.Contains(model.accumulatedAnswer, text) {
				model.accumulatedAnswer += text
			}

			// Execute any accumulated function calls and do follow-up
			localToolUses := model.executeStreamedFunctionCalls()
			logger.Debug.Printf("streamed: executed %d function calls", len(localToolUses))
			if len(localToolUses) == 0 {
				logger.Debug.Printf("streamed: completed without tool calls; this can happen when the model chooses not to call tools")
			}

			model.ResponseHandler.FinalText(model.contextId, model.prompt, model.accumulatedAnswer, localToolUses, model.modelName, nil)
			logger.Debug.Printf("streamed: FinalText sent")

			if len(localToolUses) > 0 {
				logger.Debug.Printf("streamed: sending follow-up AwaitedQuery with %d tool results", len(localToolUses))
				modifiers := &commontypes.PayloadModifiers{
					ToolUses: localToolUses,
				}
				if model.Modifiers != nil {
					modifiers.ToolGroupFilters = model.Modifiers.ToolGroupFilters
				}
				services.AwaitedQuery("", model, model.HistoryRepository, services.DefaultHistoryCount, &data.Context{Id: model.contextId}, modifiers, model.modelName)
				logger.Debug.Printf("streamed: follow-up AwaitedQuery returned")
			}

			model.streamedFunctionCalls = nil

		case "response.error":
			if msg, ok := event["message"].(string); ok && strings.TrimSpace(msg) != "" {
				logger.Debug.Printf("streamed: error event: %s", msg)
				model.ResponseHandler.RecievedText("\nError: "+msg+"\n", nil)
			}
			model.ResponseHandler.FinalText(model.contextId, model.prompt, model.accumulatedAnswer, nil, model.modelName, nil)

		case "error", "response.failed":
			msg := extractStreamErrorMessage(event)
			if strings.TrimSpace(msg) == "" {
				msg = "Request failed during streaming"
			}
			logger.Debug.Printf("streamed: failure event (%s): %s", eventType, msg)
			model.ResponseHandler.RecievedText("\nError: "+msg+"\n", nil)
			model.ResponseHandler.FinalText(model.contextId, model.prompt, model.accumulatedAnswer, nil, model.modelName, nil)

		default:
			// Ignore unknown event types to remain resilient.
			logger.Debug.Printf("streamed: ignoring event type: %s", eventType)
		}
	}
}

func (model *OpenAiResponseModel) handleStreamedOutputItem(item map[string]interface{}, isDone bool) {
	itemType, _ := item["type"].(string)
	phase := "added"
	if isDone {
		phase = "done"
	}
	logger.Debug.Printf("streamed output_item.%s type: %s", phase, itemType)
	if itemType != "function_call" {
		return
	}

	fc := &StreamedFunctionCall{}
	if id, ok := item["id"].(string); ok {
		fc.ID = id
	}
	if callID, ok := item["call_id"].(string); ok {
		fc.CallID = callID
	}
	if name, ok := item["name"].(string); ok {
		fc.Name = name
	}
	if args, ok := item["arguments"].(string); ok {
		fc.Arguments = args
	}

	if !isDone {
		model.currentFunctionCall = fc
		logger.Debug.Printf("streaming function call started: %s (call_id: %s)", fc.Name, fc.CallID)
		return
	}

	if model.currentFunctionCall != nil {
		if fc.ID == "" {
			fc.ID = model.currentFunctionCall.ID
		}
		if fc.CallID == "" {
			fc.CallID = model.currentFunctionCall.CallID
		}
		if fc.Name == "" {
			fc.Name = model.currentFunctionCall.Name
		}
		if strings.TrimSpace(fc.Arguments) == "" {
			fc.Arguments = model.currentFunctionCall.Arguments
		}
	}

	if strings.TrimSpace(fc.CallID) == "" {
		logger.Debug.Printf("streaming function call done missing call_id; skipping append")
		model.currentFunctionCall = nil
		return
	}

	if strings.TrimSpace(fc.Arguments) == "" {
		fc.Arguments = "{}"
	}

	model.appendStreamedFunctionCall(*fc)
	logger.Debug.Printf("streaming function call done from output_item: %s args=%s", fc.Name, fc.Arguments)
	model.currentFunctionCall = nil
}

func (model *OpenAiResponseModel) appendStreamedFunctionCall(fc StreamedFunctionCall) {
	for _, existing := range model.streamedFunctionCalls {
		if existing.CallID != "" && existing.CallID == fc.CallID {
			return
		}
	}
	model.streamedFunctionCalls = append(model.streamedFunctionCalls, fc)
}

func extractStreamErrorMessage(event map[string]interface{}) string {
	if errObj, ok := event["error"].(map[string]interface{}); ok {
		code, _ := errObj["code"].(string)
		msg, _ := errObj["message"].(string)
		if strings.TrimSpace(code) != "" && strings.TrimSpace(msg) != "" {
			return fmt.Sprintf("%s: %s", code, msg)
		}
		if strings.TrimSpace(msg) != "" {
			return msg
		}
	}

	if responseObj, ok := event["response"].(map[string]interface{}); ok {
		if errObj, ok := responseObj["error"].(map[string]interface{}); ok {
			code, _ := errObj["code"].(string)
			msg, _ := errObj["message"].(string)
			if strings.TrimSpace(code) != "" && strings.TrimSpace(msg) != "" {
				return fmt.Sprintf("%s: %s", code, msg)
			}
			if strings.TrimSpace(msg) != "" {
				return msg
			}
		}
	}

	if msg, ok := event["message"].(string); ok {
		return msg
	}

	return ""
}

// executeStreamedFunctionCalls runs all accumulated function calls from streaming
// and returns the tool use records for follow-up.
func (model *OpenAiResponseModel) executeStreamedFunctionCalls() []data.ToolUse {
	if len(model.streamedFunctionCalls) == 0 {
		return nil
	}

	localToolUses := []data.ToolUse{}

	for _, fc := range model.streamedFunctionCalls {
		logger.Debug.Printf("executing streamed function call: %s (call_id: %s)", fc.Name, fc.CallID)
		arguments := strings.TrimSpace(fc.Arguments)
		if arguments == "" || !json.Valid([]byte(arguments)) {
			arguments = "{}"
		}

		inputArgs := parseToolArguments(arguments)

		runner := tools.ToolRunner{
			ResponseHandler:   &model.ResponseHandler,
			HistoryRepository: &model.HistoryRepository,
			Context:           &data.Context{Id: model.contextId},
		}
		result, err := runner.ExecuteTool(data.Context{Id: model.contextId}, fc.Name, inputArgs)
		if err != nil {
			result = fmt.Sprintf("Error: %v", err)
			logger.Debug.Printf("streamed tool %s error: %v", fc.Name, err)
		} else {
			logger.Debug.Printf("streamed tool %s completed, result length: %d", fc.Name, len(result))
		}

		toolUse := data.ToolUse{
			Id:         fc.CallID,
			Name:       fc.Name,
			Input:      arguments,
			CallerType: "assistant",
			Result: data.ToolResult{
				ToolUseId: fc.CallID,
				Content:   result,
				Success:   err == nil,
			},
		}
		localToolUses = append(localToolUses, toolUse)
	}

	return localToolUses
}

// parseToolArguments converts JSON arguments to map[string]string for ExecuteTool
func parseToolArguments(arguments string) map[string]string {
	var rawArgs map[string]interface{}
	if err := json.Unmarshal([]byte(arguments), &rawArgs); err != nil {
		return map[string]string{}
	}

	inputArgs := map[string]string{}
	for key, value := range rawArgs {
		switch val := value.(type) {
		case string:
			inputArgs[key] = val
		default:
			bytes, err := json.Marshal(val)
			if err != nil {
				inputArgs[key] = fmt.Sprintf("%v", val)
			} else {
				inputArgs[key] = string(bytes)
			}
		}
	}
	return inputArgs
}

func extractEventText(event map[string]interface{}) string {
	if v, ok := event["delta"].(string); ok {
		return v
	}
	if v, ok := event["text"].(string); ok {
		return v
	}
	if item, ok := event["item"].(map[string]interface{}); ok {
		if content, ok := item["content"].([]interface{}); ok {
			for _, part := range content {
				if m, ok := part.(map[string]interface{}); ok {
					if txt, ok := m["text"].(string); ok {
						return txt
					}
					if delta, ok := m["delta"].(string); ok {
						return delta
					}
				}
			}
		}
	}
	return ""
}

func (model *OpenAiResponseModel) HandleBodyBytes(byte_list []byte) {
	logger.Debug.Printf("HandleBodyBytes called, %d bytes", len(byte_list))
	var apiResponse Response
	if err := json.Unmarshal(byte_list, &apiResponse); err != nil {
		logger.Debug.Printf("HandleBodyBytes: unmarshal error: %v", err)
		println(fmt.Sprintf("Error unmarshalling response body: %v\n", err))
	}

	logger.Debug.Printf("HandleBodyBytes: %d output items", len(apiResponse.Output))

	text := ""
	toolUses := []data.ToolUse{}
	localToolUses := []data.ToolUse{}
	toolUseByID := map[string]int{}

	for _, output := range apiResponse.Output {
		logger.Debug.Printf("HandleBodyBytes output type: %s", output.GetType())
		switch v := output.(type) {
		case ImageGenerationCall:
			unbased, err := base64.StdEncoding.DecodeString(v.Result)
			if err != nil {
				panic("Cannot decode b64")
			}

			r := bytes.NewReader(unbased)
			im, err := png.Decode(r)
			if err != nil {
				panic("Bad png")
			}

			filename := fmt.Sprintf("/Users/olofmoriya/.owl/img/%d-%s.png", model.contextId, time.Now().Format("2006-01-02:15:04:05"))
			f, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE, 0777)
			if err != nil {
				panic("Cannot open file")
			}

			png.Encode(f, im)

			err = open.Start(filename)
			if err != nil {
				fmt.Printf("Failed to open image: %v\n", err)
			}

			text += fmt.Sprintf("\n![Image](%s)\n", filename)

		case ReasoningOutput:
			for _, summary := range v.Summary {
				if summary.Text != "" {
					grey := "grey"
					model.ResponseHandler.RecievedText(summary.Text, &grey)
					model.ResponseHandler.RecievedText("\n", &grey)
					text += summary.Text + "\n"
				}
			}

		case Message:
			for i, content := range v.Content {
				text += content.Text
				if i < (len(v.Content) - 1) {
					text += "\n\n"
				}
			}

		case WebSearchCall:
			actionBytes, _ := json.Marshal(v.Action)
			action := strings.TrimSpace(string(actionBytes))
			if action == "" || action == "null" {
				action = "{}"
			}

			toolUse := data.ToolUse{
				Id:         v.ID,
				Name:       "web_search",
				Input:      action,
				CallerType: "assistant_server",
				Result: data.ToolResult{
					ToolUseId: v.ID,
					Success:   true,
				},
			}
			toolUseByID[v.ID] = len(toolUses)
			toolUses = append(toolUses, toolUse)

		case WebFetchCall:
			actionBytes, _ := json.Marshal(v.Action)
			action := strings.TrimSpace(string(actionBytes))
			if action == "" || action == "null" {
				action = "{}"
			}

			toolUse := data.ToolUse{
				Id:         v.ID,
				Name:       "web_fetch",
				Input:      action,
				CallerType: "assistant_server",
				Result: data.ToolResult{
					ToolUseId: v.ID,
					Success:   true,
				},
			}
			toolUseByID[v.ID] = len(toolUses)
			toolUses = append(toolUses, toolUse)

		case FunctionCall:
			arguments := strings.TrimSpace(v.Arguments)
			if arguments == "" || !json.Valid([]byte(arguments)) {
				arguments = "{}"
			}

			inputArgs := parseToolArguments(arguments)

			logger.Debug.Printf("HandleBodyBytes: executing function call %s (call_id: %s)", v.Name, v.CallID)
			runner := tools.ToolRunner{
				ResponseHandler:   &model.ResponseHandler,
				HistoryRepository: &model.HistoryRepository,
				Context:           &data.Context{Id: model.contextId},
			}
			result, err := runner.ExecuteTool(data.Context{Id: model.contextId}, v.Name, inputArgs)
			if err != nil {
				result = fmt.Sprintf("Error: %v", err)
			}
			logger.Debug.Printf("HandleBodyBytes: function call %s done, result length: %d", v.Name, len(result))

			toolUse := data.ToolUse{
				Id:         v.CallID,
				Name:       v.Name,
				Input:      arguments,
				CallerType: "assistant",
				Result: data.ToolResult{
					ToolUseId: v.CallID,
					Content:   result,
					Success:   err == nil,
				},
			}
			toolUses = append(toolUses, toolUse)
			localToolUses = append(localToolUses, toolUse)
		}
	}

	if len(toolUses) > 0 {
		resultPayload, _ := json.Marshal(map[string]string{
			"type":        "responses_tool_result",
			"output_text": text,
		})
		for toolUseID, idx := range toolUseByID {
			toolUses[idx].Result = data.ToolResult{
				ToolUseId: toolUseID,
				Content:   string(resultPayload),
				Success:   true,
			}
		}
	}

	logger.Debug.Printf("Final text from responses: %s", text)
	model.ResponseHandler.FinalText(model.contextId, model.prompt, text, toolUses, model.modelName, nil)

	// If there were local function calls, send the results back to the model
	// so it can produce a final answer based on tool output.
	if len(localToolUses) > 0 {
		logger.Debug.Printf("HandleBodyBytes: sending follow-up AwaitedQuery with %d tool results", len(localToolUses))
		modifiers := &commontypes.PayloadModifiers{
			ToolUses: localToolUses,
		}
		if model.Modifiers != nil {
			modifiers.ToolGroupFilters = model.Modifiers.ToolGroupFilters
		}
		services.AwaitedQuery("", model, model.HistoryRepository, services.DefaultHistoryCount, &data.Context{Id: model.contextId}, modifiers, model.modelName)
		logger.Debug.Printf("HandleBodyBytes: follow-up AwaitedQuery returned")
	}
}

func filterLocalToolUses(toolUses []data.ToolUse) []data.ToolUse {
	if len(toolUses) == 0 {
		return nil
	}
	localToolUses := make([]data.ToolUse, 0, len(toolUses))
	for _, tu := range toolUses {
		if tu.CallerType == "" || tu.CallerType == "assistant" {
			localToolUses = append(localToolUses, tu)
		}
	}
	return localToolUses
}

func (model *OpenAiResponseModel) SetResponseHandler(responseHandler commontypes.ResponseHandler) {
	model.ResponseHandler = responseHandler
}
