package models

import (
	commontypes "owl/common_types"
	"owl/data"
	claude_model "owl/models/claude"
	gemeni_model "owl/models/gemeni"
	grok_model "owl/models/grok"
	ollama_model "owl/models/ollama"
	openai_4o_model "owl/models/open-ai-4o"
	openai_base "owl/models/open-ai-base"
	open_ai_gpt_model "owl/models/open-ai-gpt"
	open_ai_responses "owl/models/open-ai-responses"
	"owl/openai_auth"
)

func GetModelForQuery(
	requestedModel string,
	context *data.Context,
	responseHandler commontypes.ResponseHandler,
	historyRepository data.HistoryRepository,
	streamMode bool,
	thinkingMode bool,
	streamThinkingMode bool,
	outputThinkingMode bool,
) (commontypes.Model, string) {

	modelToUse := requestedModel
	if modelToUse == "" && context != nil && context.PreferredModel != "" {
		modelToUse = context.PreferredModel
	}
	if modelToUse == "" {
		if openai_auth.HasCodexOAuthCredential() {
			modelToUse = "codex"
		} else {
			modelToUse = "claude"
		}
	}

	var model commontypes.Model

	switch modelToUse {
	case "gemeni":
		model = &gemeni_model.GemeniModel{
			OpenAICompatibleModel: openai_base.OpenAICompatibleModel{
				ResponseHandler:   responseHandler,
				HistoryRepository: historyRepository,
			},
		}
	case "grok":
		model = &grok_model.GrokModel{OpenAICompatibleModel: openai_base.OpenAICompatibleModel{ResponseHandler: responseHandler, HistoryRepository: historyRepository}}
	case "4o":
		model = &openai_4o_model.OpenAi4oModel{OpenAICompatibleModel: openai_base.OpenAICompatibleModel{ResponseHandler: responseHandler, HistoryRepository: historyRepository}, ModelVersion: "gpt-4o"}
	case "gpt":
		if openai_auth.HasCodexOAuthCredential() {
			model = &open_ai_responses.OpenAiResponseModel{ResponseHandler: responseHandler, HistoryRepository: historyRepository, ModelVersion: modelToUse}
		} else {
			model = &open_ai_gpt_model.OpenAIGPTModel{OpenAICompatibleModel: openai_base.OpenAICompatibleModel{ResponseHandler: responseHandler, HistoryRepository: historyRepository}, ModelVersion: modelToUse}
		}
	case "codex":
		if openai_auth.HasCodexOAuthCredential() {
			model = &open_ai_responses.OpenAiResponseModel{ResponseHandler: responseHandler, HistoryRepository: historyRepository, ModelVersion: modelToUse}
		} else {
			model = &open_ai_gpt_model.OpenAIGPTModel{OpenAICompatibleModel: openai_base.OpenAICompatibleModel{ResponseHandler: responseHandler, HistoryRepository: historyRepository}, ModelVersion: modelToUse}
		}
	case "gpt-5.4":
		model = &open_ai_gpt_model.OpenAIGPTModel{OpenAICompatibleModel: openai_base.OpenAICompatibleModel{ResponseHandler: responseHandler, HistoryRepository: historyRepository}, ModelVersion: modelToUse}
	case "gpt-5.5":
		model = &open_ai_gpt_model.OpenAIGPTModel{OpenAICompatibleModel: openai_base.OpenAICompatibleModel{ResponseHandler: responseHandler, HistoryRepository: historyRepository}, ModelVersion: modelToUse}
	case "responses":
		model = &open_ai_responses.OpenAiResponseModel{ResponseHandler: responseHandler, HistoryRepository: historyRepository, ModelVersion: "gpt"}
	case "gpt-nano":
		model = &open_ai_gpt_model.OpenAIGPTModel{OpenAICompatibleModel: openai_base.OpenAICompatibleModel{ResponseHandler: responseHandler, HistoryRepository: historyRepository}, ModelVersion: modelToUse}
	case "gpt-mini":
		model = &open_ai_gpt_model.OpenAIGPTModel{OpenAICompatibleModel: openai_base.OpenAICompatibleModel{ResponseHandler: responseHandler, HistoryRepository: historyRepository}, ModelVersion: modelToUse}
	case "gpt-chat":
		model = &open_ai_gpt_model.OpenAIGPTModel{OpenAICompatibleModel: openai_base.OpenAICompatibleModel{ResponseHandler: responseHandler, HistoryRepository: historyRepository}, ModelVersion: "gpt"}
		modelToUse = "gpt-chat"
	case "codex-chat":
		model = &open_ai_gpt_model.OpenAIGPTModel{OpenAICompatibleModel: openai_base.OpenAICompatibleModel{ResponseHandler: responseHandler, HistoryRepository: historyRepository}, ModelVersion: "codex"}
		modelToUse = "codex-chat"
	case "ollama":
		model = ollama_model.NewOllamaModel(responseHandler, historyRepository, "")
	case "qwen3":
		model = ollama_model.NewOllamaModel(responseHandler, historyRepository, "")
	case "opus":
		model = &claude_model.ClaudeModel{UseStreaming: streamMode, HistoryRepository: historyRepository, ResponseHandler: responseHandler, UseThinking: thinkingMode, StreamThought: streamThinkingMode, OutputThought: outputThinkingMode, ModelVersion: "opus"}
	case "sonnet":
		model = &claude_model.ClaudeModel{UseStreaming: streamMode, HistoryRepository: historyRepository, ResponseHandler: responseHandler, UseThinking: thinkingMode, StreamThought: streamThinkingMode, OutputThought: outputThinkingMode, ModelVersion: "sonnet"}
	case "haiku":
		model = &claude_model.ClaudeModel{UseStreaming: streamMode, HistoryRepository: historyRepository, ResponseHandler: responseHandler, UseThinking: thinkingMode, StreamThought: streamThinkingMode, OutputThought: outputThinkingMode, ModelVersion: "haiku"}
	case "claude":
		fallthrough
	default:
		model = &claude_model.ClaudeModel{UseStreaming: streamMode, HistoryRepository: historyRepository, ResponseHandler: responseHandler, UseThinking: thinkingMode, StreamThought: streamThinkingMode, OutputThought: outputThinkingMode}
		modelToUse = "claude"
	}

	return model, modelToUse
}
