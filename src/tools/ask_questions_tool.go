package tools

import (
	"encoding/json"
	"fmt"
	commontypes "owl/common_types"
	"owl/data"
	"owl/interaction"
	"owl/logger"
	"strings"
	"time"
)

type AskQuestionsTool struct{}

func (tool *AskQuestionsTool) SetHistory(repo *data.HistoryRepository, context *data.Context) {}

func (tool *AskQuestionsTool) GetName() string {
	return "ask_questions"
}

func (tool *AskQuestionsTool) GetDefinition() (Tool, string) {
	return Tool{
		Name:         tool.GetName(),
		Description:  "Ask a batch of questions in TUI mode. Provide title (optional) and questions (required). Each question requires id and question. options may be string[] or [{label:string}] (prefer 3, max 6). allow_custom and required default to true. Set allow_multiple=true when multiple choices should be selectable.",
		Groups:       []ToolGroup{ToolGroupPlanner, ToolGroupDeveloper, ToolGroupSecretary},
		Dependencies: []ToolDependency{ToolDependencyLocalExec},
		InputSchema: InputSchema{
			Type:     "object",
			Required: []string{"Questions"},
			Properties: map[string]Property{
				"Title": {
					Type:        "string",
					Description: "Optional title shown in the questionnaire view.",
				},
				"Questions": {
					Type:        "array",
					Description: "Question list. Each item requires id and question. options accepts string[] or [{label:string}].",
					Items: &Property{
						Type: "object",
						Properties: map[string]Property{
							"id":             {Type: "string", Description: "Unique question id."},
							"question":       {Type: "string", Description: "Question text."},
							"options": {
								Type:        "array",
								Description: "Options as strings. Object options are also accepted at runtime for backward compatibility.",
								Items: &Property{
									Type: "string",
								},
							},
							"allow_custom":   {Type: "boolean", Description: "Optional. Default true."},
							"allow_multiple": {Type: "boolean", Description: "Optional. Default false."},
							"required":       {Type: "boolean", Description: "Optional. Default true."},
						},
					},
				},
				"Batch": {
					Type:        "string",
					Description: "Legacy fallback. Full batch JSON string with title and questions.",
				},
			},
		},
	}, LOCAL
}

func (tool *AskQuestionsTool) GetGroups() []ToolGroup {
	return []ToolGroup{ToolGroupPlanner, ToolGroupDeveloper, ToolGroupSecretary}
}

func (tool *AskQuestionsTool) Run(i map[string]string) (string, error) {
	logger.Debug.Printf("ask_questions invoked with keys: %v", keysOf(i))

	request, err := parseQuestionBatchFromInput(i)
	if err != nil {
		logger.Debug.Printf("ask_questions parse failure: %v", err)
		return "", err
	}

	if err := validateQuestionBatch(request); err != nil {
		logger.Debug.Printf("ask_questions validation failure: %v", err)
		return "", err
	}

	if interaction.QuestionPromptChan == nil {
		logger.Debug.Printf("ask_questions failed: no TUI prompt channel")
		return "", fmt.Errorf("ask_questions requires TUI interactive mode")
	}

	resultChan := make(chan interaction.QuestionPromptResult, 1)
	prompt := interaction.QuestionPrompt{Request: request, ResponseChan: resultChan}

	select {
	case interaction.QuestionPromptChan <- prompt:
		logger.Debug.Printf("ask_questions prompt sent to TUI: title=%q questions=%d", request.Title, len(request.Questions))
	case <-time.After(3 * time.Second):
		logger.Debug.Printf("ask_questions failed: could not deliver prompt to TUI in time")
		return "", fmt.Errorf("ask_questions failed to reach TUI prompt handler")
	}

	select {
	case result := <-resultChan:
		if result.Err != nil {
			logger.Debug.Printf("ask_questions canceled/failed from TUI: %v", result.Err)
			return "", result.Err
		}
		if result.Response == nil {
			logger.Debug.Printf("ask_questions failed: TUI returned nil response")
			return "", fmt.Errorf("ask_questions returned no response")
		}
		logger.Debug.Printf("ask_questions got response with %d answers", len(result.Response.Answers))
		bytes, err := json.Marshal(result.Response)
		if err != nil {
			logger.Debug.Printf("ask_questions serialize failure: %v", err)
			return "", fmt.Errorf("failed to serialize answers: %w", err)
		}
		return string(bytes), nil
	case <-time.After(10 * time.Minute):
		logger.Debug.Printf("ask_questions timed out waiting for user input")
		return "", fmt.Errorf("ask_questions timed out waiting for user input")
	}
}

func keysOf(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func parseQuestionBatchFromInput(input map[string]string) (commontypes.QuestionBatchRequest, error) {
	if raw := strings.TrimSpace(input["Batch"]); raw != "" {
		return parseQuestionBatch(raw)
	}

	questionsRaw := strings.TrimSpace(input["Questions"])
	if questionsRaw == "" {
		return commontypes.QuestionBatchRequest{}, fmt.Errorf("Questions is required")
	}

	title := strings.TrimSpace(input["Title"])
	batchRaw := fmt.Sprintf(`{"title":%q,"questions":%s}`, title, questionsRaw)
	return parseQuestionBatch(batchRaw)
}

type rawQuestionBatch struct {
	Title     string        `json:"title"`
	Questions []rawQuestion `json:"questions"`
}

type rawQuestion struct {
	ID            string            `json:"id"`
	Question      string            `json:"question"`
	Options       []json.RawMessage `json:"options"`
	AllowCustom   *bool             `json:"allow_custom"`
	AllowMultiple *bool             `json:"allow_multiple"`
	Required      *bool             `json:"required"`
}

func parseQuestionBatch(raw string) (commontypes.QuestionBatchRequest, error) {
	var in rawQuestionBatch
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return commontypes.QuestionBatchRequest{}, fmt.Errorf("invalid Batch JSON: %w", err)
	}

	out := commontypes.QuestionBatchRequest{
		Title:     in.Title,
		Questions: make([]commontypes.QuestionItem, 0, len(in.Questions)),
	}

	for idx, q := range in.Questions {
		allowCustom := true
		if q.AllowCustom != nil {
			allowCustom = *q.AllowCustom
		}

		required := true
		if q.Required != nil {
			required = *q.Required
		}

		allowMultiple := false
		if q.AllowMultiple != nil {
			allowMultiple = *q.AllowMultiple
		}

		options, err := normalizeOptions(q.Options)
		if err != nil {
			return commontypes.QuestionBatchRequest{}, fmt.Errorf("question %d options: %w", idx+1, err)
		}

		out.Questions = append(out.Questions, commontypes.QuestionItem{
			ID:            q.ID,
			Question:      q.Question,
			Options:       options,
			AllowCustom:   allowCustom,
			AllowMultiple: allowMultiple,
			Required:      required,
		})
	}

	return out, nil
}

func normalizeOptions(raw []json.RawMessage) ([]commontypes.QuestionOption, error) {
	options := make([]commontypes.QuestionOption, 0, len(raw))
	for _, item := range raw {
		var label string
		if err := json.Unmarshal(item, &label); err == nil {
			options = append(options, commontypes.QuestionOption{Label: strings.TrimSpace(label)})
			continue
		}

		var obj struct {
			Label string `json:"label"`
		}
		if err := json.Unmarshal(item, &obj); err == nil {
			options = append(options, commontypes.QuestionOption{Label: strings.TrimSpace(obj.Label)})
			continue
		}

		return nil, fmt.Errorf("must be string[] or [{\"label\":\"...\"}]")
	}

	return options, nil
}

func validateQuestionBatch(batch commontypes.QuestionBatchRequest) error {
	if len(batch.Questions) == 0 {
		return fmt.Errorf("questions batch must include at least one question")
	}

	seenIDs := map[string]bool{}
	for idx, q := range batch.Questions {
		qID := strings.TrimSpace(q.ID)
		if qID == "" {
			return fmt.Errorf("question %d is missing id", idx+1)
		}
		if seenIDs[qID] {
			return fmt.Errorf("duplicate question id: %s", qID)
		}
		seenIDs[qID] = true

		if strings.TrimSpace(q.Question) == "" {
			return fmt.Errorf("question %s is missing question text", qID)
		}

		if len(q.Options) > 6 {
			return fmt.Errorf("question %s has too many options (max 6)", qID)
		}
		if len(q.Options) == 0 && !q.AllowCustom && q.Required {
			return fmt.Errorf("question %s has no options and disallows custom answers", qID)
		}
		for optIdx, opt := range q.Options {
			if strings.TrimSpace(opt.Label) == "" {
				return fmt.Errorf("question %s option %d is empty", qID, optIdx+1)
			}
		}
	}

	return nil
}

func init() {
	Register(&AskQuestionsTool{})
}
