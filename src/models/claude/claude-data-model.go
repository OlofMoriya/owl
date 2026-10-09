package claude_model

import "encoding/json"

type MessageBody struct {
	Model        string          `json:"model"`
	Messages     Message         `json:"messages"`
	MaxTokens    int             `json:"max_tokens"`
	System       []SystemContent `json:"system,omitempty"`
	Stream       bool            `json:"stream"`
	Thinking     *ThinkingBlock  `json:"thinking,omitempty"`
	Temp         *float32        `json:"temperature,omitempty"`
	OutputConfig *OutputConfig   `json:"output_config,omitempty"`
	Tools        []ToolModel     `json:"tools"`
}

// CacheControl enables prompt caching for content blocks
type CacheControl struct {
	Type string `json:"type"` // "ephemeral"
}

type Property struct {
	Type        string              `json:"type"`
	Description string              `json:"description,omitempty"`
	Properties  map[string]Property `json:"properties,omitempty"`
	Items       *Property           `json:"items,omitempty"`
}

type ToolModel struct {
	Value interface{}
}

func (t ToolModel) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.Value)
}

type BasicTool struct {
	Type    string `json:"type,omitempty"`
	Name    string `json:"name"`
	MaxUses int    `json:"max_uses,omitempty"`
}

type Tool struct {
	Type        string      `json:"type,omitempty"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"input_schema"`
	MaxUses     int         `json:"max_uses,omitempty"`
	Id          string      `json:"id,omitempty"`
}

// InputSchema represents the schema for tool inputs
type InputSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties"`
	Required   []string            `json:"required,omitempty"`
}

type ThinkingBlock struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

type OutputConfig struct {
	Effort string `json:"effort"`
}

// SystemContent represents system prompt content with optional caching
type SystemContent struct {
	Type         string        `json:"type"`
	Text         string        `json:"text"`
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

type Message interface {
}

type MessageResponse struct {
	Id         string            `json:"id"`
	Type       string            `json:"type"`
	Role       string            `json:"role"`
	Content    []ResponseMessage `json:"content"`
	Model      string            `json:"model"`
	StopReason string            `json:"stop_reason"`
	Usage      Usage             `json:"usage"`
}

type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

type ResponseMessage struct {
	Type      string                 `json:"type"`
	Text      string                 `json:"text,omitempty"`
	Id        string                 `json:"id,omitempty"`
	ToolUseId string                 `json:"tool_use_id,omitempty"`
	Content   interface{}            `json:"content,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Input     map[string]interface{} `json:"input,omitempty"`
	Thinking  string                 `json:"thinking,omitempty"`
	Signature string                 `json:"signature,omitempty"`
}

type Role string

type RequestMessage struct {
	Role    string    `json:"role"`
	Content []Content `json:"content"`
}

type Content interface {
}

type SourceContent struct {
	Type   string `json:"type"`
	Source Source `json:"source"`
}

type ToolUseCaller struct {
	Type   string `json:"type,omitempty"`
	ToolId string `json:"toolId,omitempty"`
}

type ToolUseContent struct {
	Type   string         `json:"type"`
	Id     string         `json:"id"`
	Name   string         `json:"name"`
	Input  interface{}    `json:"input"`
	Caller *ToolUseCaller `json:"caller,omitempty"`
}

type ServerToolResultContent struct {
	Type      string      `json:"type"`
	ToolUseId string      `json:"tool_use_id"`
	Content   interface{} `json:"content"`
}

type WebSearchResultItem struct {
	Type             string `json:"type"`
	URL              string `json:"url,omitempty"`
	Title            string `json:"title,omitempty"`
	EncryptedContent string `json:"encrypted_content,omitempty"`
	PageAge          string `json:"page_age,omitempty"`
}

type WebSearchToolResultError struct {
	Type      string `json:"type"`
	ErrorCode string `json:"error_code"`
}

type ToolResponseContent struct {
	Type         string        `json:"type"`
	Id           string        `json:"tool_use_id"`
	Content      string        `json:"content"`
	IsError      bool          `json:"is_error"`
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

type TextContent struct {
	Type         string        `json:"type"`
	Text         string        `json:"text"`
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

type TextMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type HistoricMessage struct {
	Role    string            `json:"role"`
	Content []ResponseMessage `json:"content"`
}

type SourceMessage struct {
	Role    string        `json:"role"`
	Content SourceContent `json:"content"`
}

// type ImageContent struct {
// 	Type   string `json:"type"`
// 	Source Source `json:"source"`
// }

type MediaType string
type SourceType string

const (
	Image    MediaType  = "image"
	Document SourceType = "document"
)

const (
	Jpeg   MediaType = "image/jpeg"
	Base64 MediaType = "base64"
	Png    MediaType = "image/png"
	Gif    MediaType = "image/gif"
)

type Source struct {
	Type      string    `json:"type"`
	MediaType MediaType `json:"media_type"`
	Data      string    `json:"data"`
}

type StreamEventType string

const (
	ping                StreamEventType = "ping"
	message_stop        StreamEventType = "message_stop"
	message_delta       StreamEventType = "message_delta"
	content_block_delta StreamEventType = "content_block_delta"
	content_block_stop  StreamEventType = "content_block_stop"
)

type StreamResponse struct {
	Event StreamEventType `json:"event"`
	Data  StreamData      `json:"data"`
}

type StreamData struct {
	Type  StreamEventType `json:"type"`
	Index int             `json:"index"`
	Delta StreamDelta     `json:"delta"`
}

type StreamDelta struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Thinking string `json:"thinking"`
}
