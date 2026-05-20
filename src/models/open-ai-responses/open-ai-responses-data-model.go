package open_ai_responses

import (
	"encoding/json"
	"fmt"
)

type RequestPayload struct {
	Model  string      `json:"model"`
	Input  interface{} `json:"input"`
	Tools  []Tool      `json:"tools,omitempty"`
	Stream *bool       `json:"stream,omitempty"`
}

type Tool struct {
	Type        string                 `json:"type"`
	Name        string                 `json:"name,omitempty"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

type InputMessage struct {
	Type    string `json:"type"`
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type InputFunctionCall struct {
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type InputFunctionCallOutput struct {
	Type   string `json:"type"`
	CallID string `json:"call_id"`
	Output string `json:"output"`
}

type WebSearchCall struct {
	ID     string      `json:"id"`
	Type   string      `json:"type"`
	Status string      `json:"status,omitempty"`
	Action interface{} `json:"action,omitempty"`
}

func (w WebSearchCall) GetType() string { return w.Type }

type WebFetchCall struct {
	ID     string      `json:"id"`
	Type   string      `json:"type"`
	Status string      `json:"status,omitempty"`
	Action interface{} `json:"action,omitempty"`
}

func (w WebFetchCall) GetType() string { return w.Type }

type FunctionCall struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Status    string `json:"status,omitempty"`
}

func (f FunctionCall) GetType() string { return f.Type }

type Delta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type Response struct {
	ID                 string                 `json:"id"`
	Object             string                 `json:"object"`
	CreatedAt          int64                  `json:"created_at"`
	Status             string                 `json:"status"`
	Error              *string                `json:"error"`
	IncompleteDetails  *string                `json:"incomplete_details"`
	Instructions       *string                `json:"instructions"`
	MaxOutputTokens    *int                   `json:"max_output_tokens"`
	Model              string                 `json:"model"`
	Output             []OutputItem           `json:"output"`
	ParallelToolCalls  bool                   `json:"parallel_tool_calls"`
	PreviousResponseID *string                `json:"previous_response_id"`
	Reasoning          Reasoning              `json:"reasoning"`
	Store              bool                   `json:"store"`
	Temperature        float64                `json:"temperature"`
	Text               TextFormat             `json:"text"`
	ToolChoice         string                 `json:"tool_choice"`
	Tools              []interface{}          `json:"tools"`
	TopP               float64                `json:"top_p"`
	Truncation         string                 `json:"truncation"`
	Usage              Usage                  `json:"usage"`
	User               *string                `json:"user"`
	Metadata           map[string]interface{} `json:"metadata"`
}

// OutputItem is the interface all output types implement
type OutputItem interface {
	GetType() string
}

// ImageGenerationCall represents an image generation output
type ImageGenerationCall struct {
	ID            string `json:"id"`
	Type          string `json:"type"`
	Status        string `json:"status"`
	Background    string `json:"background"`
	OutputFormat  string `json:"output_format"`
	Quality       string `json:"quality"`
	Result        string `json:"result"`
	RevisedPrompt string `json:"revised_prompt"`
	Size          string `json:"size"`
}

func (i ImageGenerationCall) GetType() string { return i.Type }

// ReasoningSummaryItem represents individual summary text entries
type ReasoningSummaryItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ReasoningOutput represents a reasoning output block
type ReasoningOutput struct {
	ID      string                 `json:"id"`
	Type    string                 `json:"type"`
	Summary []ReasoningSummaryItem `json:"summary"`
}

func (ro ReasoningOutput) GetType() string { return ro.Type }

// Message represents a message output
type Message struct {
	ID      string        `json:"id"`
	Type    string        `json:"type"`
	Status  string        `json:"status"`
	Content []ContentItem `json:"content"`
	Role    string        `json:"role"`
}

func (m Message) GetType() string { return m.Type }

// ContentItem for message content
type ContentItem struct {
	Type        string        `json:"type"`
	Annotations []interface{} `json:"annotations"`
	Logprobs    []interface{} `json:"logprobs"`
	Text        string        `json:"text"`
}

// Custom unmarshaling for Response — uses alias to populate all fields,
// then dispatches output items by type, skipping unknown types.
func (r *Response) UnmarshalJSON(data []byte) error {
	// Alias avoids infinite recursion when calling Unmarshal on the same type.
	type Alias Response
	aux := &struct {
		Output []json.RawMessage `json:"output"`
		*Alias
	}{
		Alias: (*Alias)(r),
	}

	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	// Process each output item
	r.Output = make([]OutputItem, 0, len(aux.Output))

	for _, raw := range aux.Output {
		// Peek at the type field
		var typeCheck struct {
			Type string `json:"type"`
		}

		if err := json.Unmarshal(raw, &typeCheck); err != nil {
			return err
		}

		switch typeCheck.Type {
		case "image_generation_call":
			var img ImageGenerationCall
			if err := json.Unmarshal(raw, &img); err != nil {
				return err
			}
			r.Output = append(r.Output, img)

		case "reasoning":
			var ro ReasoningOutput
			if err := json.Unmarshal(raw, &ro); err != nil {
				return err
			}
			r.Output = append(r.Output, ro)

		case "message":
			var msg Message
			if err := json.Unmarshal(raw, &msg); err != nil {
				return err
			}
			r.Output = append(r.Output, msg)

		case "web_search_call":
			var ws WebSearchCall
			if err := json.Unmarshal(raw, &ws); err != nil {
				return err
			}
			r.Output = append(r.Output, ws)

		case "web_fetch_call":
			var wf WebFetchCall
			if err := json.Unmarshal(raw, &wf); err != nil {
				return err
			}
			r.Output = append(r.Output, wf)

		case "function_call":
			var fc FunctionCall
			if err := json.Unmarshal(raw, &fc); err != nil {
				return err
			}
			r.Output = append(r.Output, fc)

		default:
			// Skip truly unknown output types to stay forward-compatible
			fmt.Printf("skipping unknown output type: %s\n", typeCheck.Type)
		}
	}

	return nil
}

type Reasoning struct {
	Effort  *string `json:"effort"`
	Summary *string `json:"summary"`
}

type TextFormat struct {
	Format FormatType `json:"format"`
}

type FormatType struct {
	Type string `json:"type"`
}

type Usage struct {
	InputTokens         int                 `json:"input_tokens"`
	InputTokensDetails  InputTokensDetails  `json:"input_tokens_details"`
	OutputTokens        int                 `json:"output_tokens"`
	OutputTokensDetails OutputTokensDetails `json:"output_tokens_details"`
	TotalTokens         int                 `json:"total_tokens"`
}

type InputTokensDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

type OutputTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

type ChatCompletionChunkChoice struct {
	Index        int         `json:"index"`
	Delta        Delta       `json:"delta"`
	Logprobs     interface{} `json:"logprobs"`
	FinishReason *string     `json:"finish_reason,omitempty"`
}

type ChatCompletionChunk struct {
	ID                string                      `json:"id"`
	Object            string                      `json:"object"`
	Created           int64                       `json:"created"`
	Model             string                      `json:"model"`
	SystemFingerprint string                      `json:"system_fingerprint"`
	Choices           []ChatCompletionChunkChoice `json:"choices"`
}
