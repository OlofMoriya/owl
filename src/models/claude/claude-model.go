package claude_model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	commontypes "owl/common_types"
	data "owl/data"
	"owl/logger"
	"owl/mode"
	"owl/services"
	"owl/tools"
	"sort"
	"strings"

	"github.com/fatih/color"
)

type ClaudeModel struct {
	HistoryRepository data.HistoryRepository
	ResponseHandler   commontypes.ResponseHandler
	Prompt            string
	AccumulatedAnswer string
	Context           *data.Context
	ModelVersion      string
	OutputThought     bool
	StreamThought     bool
	UseThinking       bool
	UseStreaming      bool

	//Track streamed content
	CurrentEvent           string
	CurrentToolUse         *StreamedToolUse
	StreamedToolUses       []StreamedToolUse
	StreamedToolResultById map[string]data.ToolResult
	Modifiers              *commontypes.PayloadModifiers
	PendingUsage           *commontypes.TokenUsage
}

type StreamedToolUse struct {
	Id         string
	Name       string
	Input      string
	CallerType string
}

func (model *ClaudeModel) SetResponseHandler(responseHandler commontypes.ResponseHandler) {
	model.ResponseHandler = responseHandler

}

func (model *ClaudeModel) CreateRequest(context *data.Context, prompt string, streaming bool, history []data.History, modifiers *commontypes.PayloadModifiers) *http.Request {

	logger.Debug.Printf("\nMODEL USE: creating claude payload: %s", model.ModelVersion)

	var model_version string
	switch model.ModelVersion {
	case "opus":
		model_version = "claude-opus-5-5"
	case "sonnet":
		model_version = "claude-sonnet-5-5"
	case "haiku":
		model_version = "claude-haiku-4-5-20251001"
	default:
		model_version = "claude-sonnet-5-5"
	}

	payload := createClaudePayload(prompt, streaming, history, model_version, model.UseThinking, context, modifiers)
	model.Prompt = prompt
	model.AccumulatedAnswer = ""
	model.Context = context
	model.StreamedToolUses = nil
	model.StreamedToolResultById = map[string]data.ToolResult{}

	request := createClaudeRequest(payload)
	model.Modifiers = modifiers
	model.PendingUsage = nil

	return request
}

func (model *ClaudeModel) HandleStreamedLine(line []byte) {
	responseLine := string(line)

	// Capture event type
	if strings.HasPrefix(responseLine, "event: ") {
		event, _ := strings.CutPrefix(responseLine, "event: ")
		model.CurrentEvent = strings.TrimSpace(event)
		return
	}

	// Parse data based on current event
	if strings.HasPrefix(responseLine, "data: ") {
		payload, _ := strings.CutPrefix(responseLine, "data: ")

		switch model.CurrentEvent {
		case "content_block_start":
			model.handleContentBlockStart(payload)
		case "content_block_delta":
			model.handleContentBlockDelta(payload)
		case "content_block_stop":
			if model.CurrentToolUse != nil {
				model.StreamedToolUses = append(model.StreamedToolUses, *model.CurrentToolUse)
				model.CurrentToolUse = nil
			}
			model.ResponseHandler.RecievedText("\n", nil)
		case "message_delta":
			model.handleMessageDelta(payload)
		case "message_stop":
			fakeResponse := MessageResponse{
				Content: []ResponseMessage{},
			}

			logger.Debug.Printf("Message stop with toolUsesSaved: %v", model.StreamedToolUses)

			for _, toolUse := range model.StreamedToolUses {
				var inputMap map[string]interface{}
				err := json.Unmarshal([]byte(toolUse.Input), &inputMap)
				if err != nil {
					inputMap = map[string]interface{}{}
				}

				blockType := "tool_use"
				if toolUse.CallerType == "assistant_server" {
					blockType = "server_tool_use"
				}

				content := ResponseMessage{
					Id:    toolUse.Id,
					Name:  toolUse.Name,
					Input: inputMap,
					Type:  blockType,
				}
				fakeResponse.Content = append(fakeResponse.Content, content)
			}

			for toolUseId, result := range model.StreamedToolResultById {
				var resultContent interface{}
				if err := json.Unmarshal([]byte(result.Content), &resultContent); err != nil {
					resultContent = result.Content
				}
				fakeResponse.Content = append(fakeResponse.Content, ResponseMessage{
					Type:      "web_search_tool_result",
					ToolUseId: toolUseId,
					Content:   resultContent,
				})
			}

			logger.Debug.Printf("Message stop with fakedResponse.Content: %v", fakeResponse.Content)
			toolUses, localToolUses := model.collectToolUses(fakeResponse)

			usage := model.PendingUsage
			model.ResponseHandler.FinalText(model.Context.Id, model.Prompt, model.AccumulatedAnswer, toolUses, model.ModelVersion, usage)
			model.PendingUsage = nil

			if len(localToolUses) > 0 {
				// Continue conversation with tool results
				services.AwaitedQuery("", model, model.HistoryRepository, 1000, model.Context, &commontypes.PayloadModifiers{
					ToolUses:         localToolUses,
					ToolGroupFilters: model.Modifiers.ToolGroupFilters,
				}, model.ModelVersion)
			}

			model.StreamedToolUses = nil
			model.StreamedToolResultById = map[string]data.ToolResult{}
		}
	}
}

func (model *ClaudeModel) handleContentBlockStart(payload string) {
	var response struct {
		Type         string `json:"type"`
		Index        int    `json:"index"`
		ContentBlock struct {
			Type      string      `json:"type"`
			Id        string      `json:"id,omitempty"`
			Name      string      `json:"name,omitempty"`
			ToolUseId string      `json:"tool_use_id,omitempty"`
			Content   interface{} `json:"content,omitempty"`
		} `json:"content_block"`
	}

	if err := json.Unmarshal([]byte(payload), &response); err != nil {
		println(fmt.Sprintf("Error unmarshalling content_block_start: %v", err))
		return
	}

	if response.ContentBlock.Type == "tool_use" || response.ContentBlock.Type == "server_tool_use" {
		callerType := "assistant"
		if response.ContentBlock.Type == "server_tool_use" {
			callerType = "assistant_server"
		}

		model.CurrentToolUse = &StreamedToolUse{}
		model.CurrentToolUse.Id = response.ContentBlock.Id
		model.CurrentToolUse.Name = response.ContentBlock.Name
		model.CurrentToolUse.Input = ""
		model.CurrentToolUse.CallerType = callerType
		logger.Debug.Printf("starting streaming tool use block")
	}

	if response.ContentBlock.Type == "web_search_tool_result" {
		normalized, ok := normalizeWebSearchToolResultContent(response.ContentBlock.Content)
		if !ok {
			return
		}

		bytes, err := json.Marshal(normalized)
		if err != nil {
			return
		}

		toolUseId := response.ContentBlock.ToolUseId
		if toolUseId == "" {
			return
		}

		model.StreamedToolResultById[toolUseId] = data.ToolResult{
			ToolUseId: toolUseId,
			Content:   string(bytes),
			Success:   !webSearchResultHasError(bytes),
		}
	}
}

func (model *ClaudeModel) handleContentBlockDelta(payload string) {
	var response struct {
		Type  string `json:"type"`
		Index int    `json:"index"`
		Delta struct {
			Type        string `json:"type"`
			Text        string `json:"text,omitempty"`
			Thinking    string `json:"thinking,omitempty"`
			PartialJson string `json:"partial_json,omitempty"`
		} `json:"delta"`
	}

	if err := json.Unmarshal([]byte(payload), &response); err != nil {
		println(fmt.Sprintf("Error unmarshalling content_block_delta: %v", err))
		return
	}

	if response.Delta.Type == "input_json_delta" {
		// Accumulate tool input
		if model.CurrentToolUse != nil {
			model.CurrentToolUse.Input += response.Delta.PartialJson
		}
	} else if response.Delta.Type == "text_delta" {
		model.AccumulatedAnswer = model.AccumulatedAnswer + response.Delta.Text
		model.ResponseHandler.RecievedText(response.Delta.Text, nil)
	} else if response.Delta.Type == "thinking_delta" {
		model.AccumulatedAnswer = model.AccumulatedAnswer + response.Delta.Thinking
		if model.StreamThought {
			color := "grey"
			model.ResponseHandler.RecievedText(response.Delta.Thinking, &color)
		}
	} else {
		logger.Debug.Printf("unhandled content block delta arrived of type: %v", response.Delta.Type)
	}
}

func (model *ClaudeModel) handleMessageDelta(payload string) {
	var response struct {
		Type  string `json:"type"`
		Delta struct {
			StopReason string `json:"stop_reason"`
		} `json:"delta"`
		Usage Usage `json:"usage"`
	}

	if err := json.Unmarshal([]byte(payload), &response); err != nil {
		println(fmt.Sprintf("Error unmarshalling message_delta: %v", err))
		return
	}

	if response.Delta.StopReason == "tool_use" && model.CurrentToolUse != nil {
		alreadyTracked := false
		for _, toolUse := range model.StreamedToolUses {
			if toolUse.Id == model.CurrentToolUse.Id {
				alreadyTracked = true
				break
			}
		}
		if !alreadyTracked {
			model.StreamedToolUses = append(model.StreamedToolUses, *model.CurrentToolUse)
		}
		logger.Debug.Printf("message_delta reached tool_use stop reason with active tool %s", model.CurrentToolUse.Id)
	}

	if usage := claudeUsageToTokenUsage(response.Usage); usage != nil {
		model.PendingUsage = usage
	}
	logger.Debug.Printf("message delta arrived with data: %v", payload)
}

func (model *ClaudeModel) HandleBodyBytes(bytes []byte) {
	var apiResponse MessageResponse

	if err := json.Unmarshal(bytes, &apiResponse); err != nil {
		println(fmt.Sprintf("Error unmarshalling response body: %v\n", err))
		logger.Debug.Println(err)
	}

	logger.Debug.Println("response:")
	logger.Debug.Printf("%v", string(bytes))
	// logger.Debug.Printf("%v", apiResponse)

	responseText := ""
	for _, content := range apiResponse.Content {
		if content.Type == "text" {
			responseText += fmt.Sprintf("\n%s", content.Text)
		}
	}

	toolUses, localToolUses := model.collectToolUses(apiResponse)

	usage := claudeUsageToTokenUsage(apiResponse.Usage)
	model.ResponseHandler.FinalText(model.Context.Id, model.Prompt, responseText, toolUses, model.ModelVersion, usage)
	model.PendingUsage = nil

	if len(localToolUses) > 0 {
		// Continue conversation with tool results
		services.AwaitedQuery("", model, model.HistoryRepository, 1000, model.Context, &commontypes.PayloadModifiers{
			ToolUses:         localToolUses,
			ToolGroupFilters: model.Modifiers.ToolGroupFilters,
		}, model.ModelVersion)
	}
}

func (model *ClaudeModel) collectToolUses(apiResponse MessageResponse) ([]data.ToolUse, []data.ToolUse) {
	localToolUses := model.handleToolCalls(apiResponse)
	assistantToolUses := model.handleAssistantSideToolCallsParsing(apiResponse)

	allToolUses := make([]data.ToolUse, 0, len(localToolUses)+len(assistantToolUses))
	allToolUses = append(allToolUses, localToolUses...)
	allToolUses = append(allToolUses, assistantToolUses...)

	return allToolUses, localToolUses
}

func (model *ClaudeModel) handleToolCalls(apiResponse MessageResponse) []data.ToolUse {
	toolUses := []data.ToolUse{}

	for _, content := range apiResponse.Content {
		if content.Type != "tool_use" {
			continue
		}

		bytes, err := json.Marshal(content.Input)
		if err != nil {
			logger.Debug.Println(err)
			logger.Screen("error marshalling content from tool use", color.RGB(150, 150, 150))
			continue
		}

		toolUse := data.ToolUse{
			Id:         content.Id,
			Name:       content.Name,
			Input:      string(bytes),
			CallerType: "assistant",
		}
		response, err := model.useTool(content)
		toolUse.Result.Content = response.Response
		toolUse.Result.ToolUseId = toolUse.Id

		if err != nil {
			toolUse.Result.Success = false
			logger.Debug.Println(err)
		} else {
			toolUse.Result.Success = true
		}

		toolUses = append(toolUses, toolUse)
	}

	return toolUses
}

func (model *ClaudeModel) handleAssistantSideToolCallsParsing(apiResponse MessageResponse) []data.ToolUse {
	toolUses := []data.ToolUse{}
	toolUseById := map[string]int{}

	for _, content := range apiResponse.Content {
		switch content.Type {
		case "server_tool_use":
			bytes, err := json.Marshal(content.Input)
			if err != nil {
				logger.Debug.Println(err)
				continue
			}

			toolUse := data.ToolUse{
				Id:         content.Id,
				Name:       content.Name,
				Input:      string(bytes),
				CallerType: "assistant_server",
				Result: data.ToolResult{
					ToolUseId: content.Id,
					Success:   true,
				},
			}

			toolUseById[toolUse.Id] = len(toolUses)
			toolUses = append(toolUses, toolUse)

		case "web_search_tool_result":
			toolUseId := content.ToolUseId
			if toolUseId == "" {
				continue
			}

			normalized, ok := normalizeWebSearchToolResultContent(content.Content)
			if !ok {
				normalized = map[string]interface{}{
					"type":       "web_search_tool_result_error",
					"error_code": "unavailable",
				}
			}

			resultBytes, err := json.Marshal(normalized)
			if err != nil {
				logger.Debug.Printf("failed to marshal web search result: %v", err)
				continue
			}

			result := data.ToolResult{
				ToolUseId: toolUseId,
				Content:   string(resultBytes),
				Success:   !webSearchResultHasError(resultBytes),
			}

			if idx, ok := toolUseById[toolUseId]; ok {
				toolUses[idx].Result = result
			} else {
				toolUses = append(toolUses, data.ToolUse{
					Id:         toolUseId,
					Name:       "web_search",
					Input:      "{}",
					CallerType: "assistant_server",
					Result:     result,
				})
				toolUseById[toolUseId] = len(toolUses) - 1
			}
		}
	}

	return toolUses
}

func (model *ClaudeModel) useTool(content ResponseMessage) (commontypes.ToolResponse, error) {
	runner := tools.ToolRunner{ResponseHandler: &model.ResponseHandler, HistoryRepository: &model.HistoryRepository, Context: model.Context}
	args := map[string]string{}
	for key, value := range content.Input {
		switch v := value.(type) {
		case string:
			args[key] = v
		default:
			bytes, err := json.Marshal(v)
			if err != nil {
				msg := fmt.Sprintf("%s: invalid argument %s (%v)", content.Name, key, err)
				return commontypes.ToolResponse{Id: content.Id, Response: msg}, fmt.Errorf("could not marshal tool arg %s: %w", key, err)
			}
			args[key] = string(bytes)
		}
	}

	result, err := runner.ExecuteTool(*model.Context, content.Name, args)

	if result != "" && err == nil {
		return commontypes.ToolResponse{
			Response:        result,
			Id:              content.Id,
			ResponseMessage: content,
		}, nil
	}

	if err != nil {
		msg := fmt.Sprintf("%s failed: %v", content.Name, err)
		return commontypes.ToolResponse{Id: content.Id, Response: msg}, err
	}

	msg := fmt.Sprintf("%s failed: empty response", content.Name)
	return commontypes.ToolResponse{Id: content.Id, Response: msg}, fmt.Errorf("no response returned from tool: %s", content.Name)
}

func getCacheControl() *CacheControl {
	return &CacheControl{Type: "ephemeral"}
}

func webSearchResultHasError(resultBytes []byte) bool {
	if len(resultBytes) == 0 {
		return true
	}

	var arr []map[string]interface{}
	if err := json.Unmarshal(resultBytes, &arr); err == nil {
		for _, item := range arr {
			if v, ok := item["type"].(string); ok && v == "web_search_tool_result_error" {
				return true
			}
		}
		return false
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(resultBytes, &obj); err == nil {
		if v, ok := obj["type"].(string); ok && v == "web_search_tool_result_error" {
			return true
		}
	}

	return false
}

func normalizeWebSearchToolResultContent(raw interface{}) (interface{}, bool) {
	if raw == nil {
		return nil, false
	}

	switch v := raw.(type) {
	case []interface{}:
		return v, true
	case map[string]interface{}:
		return v, true
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return nil, false
		}
		var parsed interface{}
		if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
			return nil, false
		}
		return normalizeWebSearchToolResultContent(parsed)
	default:
		bytes, err := json.Marshal(v)
		if err != nil {
			return nil, false
		}
		var parsed interface{}
		if err := json.Unmarshal(bytes, &parsed); err != nil {
			return nil, false
		}
		return normalizeWebSearchToolResultContent(parsed)
	}
}

func createClaudePayload(prompt string, streamed bool, history []data.History, model string, useThinking bool, context *data.Context, modifiers *commontypes.PayloadModifiers) MessageBody {
	logger.Debug.Printf("crateClaudePayload called with responseCount: %d and history count: %d", len(modifiers.ToolUses), len(history))

	messages := []Message{}
	toolCacheTargets := selectToolCacheTargets(history)
	userWithoutToolCount := 0
	userPromptCached := false

	// Process history and handle tool results
	for i, h := range history {

		//user
		if h.Prompt != "" {
			textContent := TextContent{
				Type: "text",
				Text: h.Prompt,
			}
			if len(h.ToolUse) == 0 {
				userWithoutToolCount++
				if !userPromptCached && userWithoutToolCount == 2 {
					textContent.CacheControl = getCacheControl()
					userPromptCached = true
				}
			}

			messages = append(messages, RequestMessage{
				Role:    "user",
				Content: []Content{textContent},
			})
		}

		assistantContent := []Content{}
		if h.Response != "" {
			assistantContent = append(assistantContent, TextContent{Type: "text", Text: h.Response})
		}
		for _, tu := range h.ToolUse {
			var parsedInput map[string]interface{}
			if err := json.Unmarshal([]byte(tu.Input), &parsedInput); err != nil || parsedInput == nil {
				parsedInput = map[string]interface{}{}
			}

			if tu.CallerType == "assistant_server" {
				assistantContent = append(assistantContent, ToolUseContent{
					Type:  "server_tool_use",
					Id:    tu.Id,
					Name:  tu.Name,
					Input: parsedInput,
				})

				var providerContent interface{}
				if err := json.Unmarshal([]byte(tu.Result.Content), &providerContent); err != nil {
					providerContent = nil
				}
				normalizedProviderContent, ok := normalizeWebSearchToolResultContent(providerContent)
				if !ok {
					normalizedProviderContent = map[string]interface{}{
						"type":       "web_search_tool_result_error",
						"error_code": "unavailable",
					}
				}

				assistantContent = append(assistantContent, ServerToolResultContent{
					Type:      "web_search_tool_result",
					ToolUseId: tu.Id,
					Content:   normalizedProviderContent,
				})
			} else {
				assistantContent = append(assistantContent, ToolUseContent{
					Type:  "tool_use",
					Id:    tu.Id,
					Name:  tu.Name,
					Input: parsedInput,
				})
			}
		}

		if len(assistantContent) > 0 {
			messages = append(messages, RequestMessage{
				Role:    "assistant",
				Content: assistantContent,
			})
		}

		//possible answers for tools

		if len(h.ToolUse) > 0 {
			toolResultContent := []Content{}
			cacheToolResult := toolCacheTargets[i]
			localToolCount := 0
			for _, tr := range h.ToolUse {
				if tr.CallerType != "assistant_server" {
					localToolCount++
				}
			}
			localIndex := 0

			for _, tr := range h.ToolUse {
				if tr.CallerType == "assistant_server" {
					continue
				}
				content := ToolResponseContent{
					Type:    "tool_result",
					Content: tr.Result.Content,
					IsError: !tr.Result.Success,
					Id:      tr.Id,
				}
				if cacheToolResult && localToolCount > 0 && localIndex == localToolCount-1 {
					content.CacheControl = getCacheControl()
				}

				toolResultContent = append(toolResultContent, content)
				localIndex++
			}
			if len(toolResultContent) > 0 {
				messages = append(messages, RequestMessage{Role: "user", Content: toolResultContent})
			}
		}
	}

	if modifiers.Image {
		imageMessage := createImageMessage(prompt)
		messages = append(messages, imageMessage)
	} else if modifiers.Pdf != "" {
		imageMessage := createPdfMessage(prompt, *modifiers)
		messages = append(messages, imageMessage)
	} else {
		if prompt != "" {
			userContent := TextContent{
				Type: "text",
				Text: prompt,
			}
			messages = append(messages, RequestMessage{
				Role:    "user",
				Content: []Content{userContent},
			})
		}
	}

	logger.Debug.Printf("Messages length: %d", len(messages))

	payload := MessageBody{
		Model:     model,
		Messages:  messages,
		MaxTokens: 20000,
		Stream:    streamed,
	}

	toolsList := tools.GetCustomTools(mode.Mode, modifiers.ToolGroupFilters...)
	sort.Slice(toolsList, func(i, j int) bool {
		return toolsList[i].Name < toolsList[j].Name
	})

	payload.Tools = make([]ToolModel, len(toolsList))
	for i, tool := range toolsList {
		payload.Tools[i] = ToolModel{Value: Tool{
			Type:        tool.Type,
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: InputSchema{
				Type:       tool.InputSchema.Type,
				Properties: toClaudeToolProperties(tool.InputSchema.Properties),
				Required:   tool.InputSchema.Required,
			},
			MaxUses: tool.MaxUses,
		}}
	}

	if modifiers.Web {
		payload.Tools = append(payload.Tools, ToolModel{Value: getWebSearchTool()})
	}

	// Handle system prompt with caching - ALWAYS cache it
	if context != nil && context.SystemPrompt != "" {
		systemContent := SystemContent{
			Type: "text",
			Text: context.SystemPrompt,
		}
		payload.System = []SystemContent{systemContent}
	}

	adaptiveThinking := strings.HasPrefix(model, "claude-sonnet-5-") ||
		strings.HasPrefix(model, "claude-opus-5-") ||
		model == "claude-sonnet-4-6" || model == "claude-opus-4-6"
	if !adaptiveThinking {
		temperature := float32(0)
		payload.Temp = &temperature
	}
	if useThinking {
		if adaptiveThinking {
			payload.Thinking = &ThinkingBlock{Type: "adaptive"}
			payload.OutputConfig = &OutputConfig{Effort: "medium"}
		} else {
			payload.Thinking = &ThinkingBlock{
				Type:         "enabled",
				BudgetTokens: 2000,
			}
			temperature := float32(1)
			payload.Temp = &temperature
		}
	}

	// logger.Debug.Println("FULL PAYLOAD:")
	// logger.Debug.Printf("\n----\n\n%v\n\n------\n", payload)
	return payload
}

func toClaudeToolProperties(props map[string]tools.Property) map[string]Property {
	result := make(map[string]Property, len(props))
	for key, prop := range props {
		mapped := Property{
			Type:        prop.Type,
			Description: prop.Description,
		}
		if len(prop.Properties) > 0 {
			mapped.Properties = toClaudeToolProperties(prop.Properties)
		}
		if prop.Items != nil {
			mappedItem := toClaudeToolProperty(*prop.Items)
			mapped.Items = &mappedItem
		}
		result[key] = mapped
	}
	return result
}

func toClaudeToolProperty(prop tools.Property) Property {
	mapped := Property{
		Type:        prop.Type,
		Description: prop.Description,
	}
	if len(prop.Properties) > 0 {
		mapped.Properties = toClaudeToolProperties(prop.Properties)
	}
	if prop.Items != nil {
		mappedItem := toClaudeToolProperty(*prop.Items)
		mapped.Items = &mappedItem
	}
	return mapped
}

func selectToolCacheTargets(history []data.History) map[int]bool {
	indices := make([]int, 0, len(history))
	for i := range history {
		if len(history[i].ToolUse) > 0 {
			indices = append(indices, i)
		}
	}
	targets := make(map[int]bool)
	for i := len(indices) - 1; i >= 0 && len(targets) < 2; i-- {
		targets[indices[i]] = true
	}
	return targets
}

func claudeUsageToTokenUsage(u Usage) *commontypes.TokenUsage {
	logger.Debug.Printf("token usage captured %+v", u)

	if u.InputTokens == 0 && u.OutputTokens == 0 && u.CacheReadInputTokens == 0 && u.CacheCreationInputTokens == 0 {
		return nil
	}
	return &commontypes.TokenUsage{
		PromptTokens:     u.InputTokens,
		CompletionTokens: u.OutputTokens,
		CacheReadTokens:  u.CacheReadInputTokens,
		CacheWriteTokens: u.CacheCreationInputTokens,
	}
}

func createImageMessage(prompt string) RequestMessage {
	image, err := services.GetImageFromClipboard()
	if err != nil {
		panic(fmt.Sprintf("could not get image from clipboard, %v", err))
	}
	base64, err := services.ImageToBase64(image)
	if err != nil {
		panic(fmt.Sprintf("could not get base64 from image, %v", err))
	}

	imageMessage := RequestMessage{Role: "user", Content: []Content{
		TextContent{Type: "text", Text: prompt},
		SourceContent{Type: "image", Source: Source{
			Type:      string(Base64),
			MediaType: "image/png",
			Data:      base64,
		}},
	}}
	return imageMessage
}

func createPdfMessage(prompt string, modifiers commontypes.PayloadModifiers) RequestMessage {
	base64, err := services.ReadPDFAsBase64(modifiers.Pdf)
	if err != nil {
		panic(fmt.Sprintf("could not get base64 from pdf, %v", err))
	}
	imageMessage := RequestMessage{Role: "user", Content: []Content{
		TextContent{Type: "text", Text: prompt},
		SourceContent{Type: "document", Source: Source{
			Type:      string(Base64),
			MediaType: "application/pdf",
			Data:      base64,
		}},
	}}
	return imageMessage
}

func getWebSearchTool() BasicTool {
	return BasicTool{
		Type:    "web_search_20250305",
		Name:    "web_search",
		MaxUses: 2,
	}
}

func createClaudeRequest(payload MessageBody) *http.Request {
	apiKey, ok := os.LookupEnv("CLAUDE_API_KEY")
	if !ok {
		panic(fmt.Errorf("Could not fetch api key"))
	}

	jsonpayload, err := json.Marshal(payload)
	if err != nil {
		panic("failed to marshal payload")
	}
	logger.Debug.Println("FULL JSON PAYLOAD:")
	logger.Debug.Printf("\n%s", jsonpayload)

	url := "https://api.anthropic.com/v1/messages"

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonpayload))
	if err != nil {
		panic("failed to create request")
	}

	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", "prompt-caching-2024-07-31")

	return req
}
