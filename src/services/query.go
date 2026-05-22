package services

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net/http"
	"owl/common_types"
	"owl/data"
	"owl/logger"
	"strings"

	"github.com/fatih/color"
)

type awaitedQueryFunc func(prompt string, model commontypes.Model, historyRepository data.HistoryRepository, historyCount int, context *data.Context, modifiers *commontypes.PayloadModifiers, modelName string)

var awaitedQueryHook awaitedQueryFunc = awaitedQueryImplementation

const DefaultHistoryCount = 1000

func buildHTTPStatusError(statusCode int, responseBody []byte) error {
	body := strings.TrimSpace(string(responseBody))
	if body == "" {
		return fmt.Errorf("received non-OK response status: %d", statusCode)
	}
	return fmt.Errorf("received non-OK response status: %d body: %s", statusCode, body)
}

func looksLikeSSEBody(body []byte) bool {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return false
	}
	return strings.HasPrefix(trimmed, "event:") || strings.HasPrefix(trimmed, "data:")
}

// SetAwaitedQueryHook overrides the default awaited query behavior (used in tests)
func SetAwaitedQueryHook(fn awaitedQueryFunc) {
	if fn == nil {
		awaitedQueryHook = awaitedQueryImplementation
		return
	}
	awaitedQueryHook = fn
}

func AwaitedQuery(prompt string, model commontypes.Model, historyRepository data.HistoryRepository, historyCount int, context *data.Context, modifiers *commontypes.PayloadModifiers, modelName string) {
	awaitedQueryHook(prompt, model, historyRepository, historyCount, context, modifiers, modelName)
}

func awaitedQueryImplementation(prompt string, model commontypes.Model, historyRepository data.HistoryRepository, historyCount int, context *data.Context, modifiers *commontypes.PayloadModifiers, modelName string) {

	logger.Screen("sending awaited query", color.RGB(150, 150, 150))

	trimmedPrompt := strings.TrimSpace(prompt)
	hasToolResponses := false
	if modifiers != nil && len(modifiers.ToolUses) > 0 {
		hasToolResponses = true
	}
	if trimmedPrompt == "" && !hasToolResponses {
		logger.Screen("no prompt or tool response, skipping awaited query", color.RGB(250, 150, 150))
		logger.Debug.Printf("skipping awaited query due to empty input. modifiers=%+v", modifiers)
		return
	}

	history := []data.History{}
	if historyCount > 0 {
		// logger.Debug.Printf("Fetching history for HistoryRepository: >%v<, with context: >%v<", historyRepository, context)
		h, err := historyRepository.GetHistoryByContextId(context.Id, historyCount)
		if err != nil {
			logger.Debug.Printf("error while fetching history for context: %v", err)
			logger.Screen(fmt.Sprintf("error while fetching history for context: %v", err), color.RGB(250, 100, 100))
		}

		// Filter out archived history
		for _, entry := range h {
			if !entry.Archived {
				history = append(history, entry)
			}
		}
	}

	req := model.CreateRequest(context, prompt, false, history, modifiers)
	logger.Debug.Printf("sending req: %v", req)
	if authSource := strings.TrimSpace(req.Header.Get("X-Owl-Auth-Source")); authSource != "" {
		logger.Debug.Printf("query auth source: %s", authSource)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		panic(fmt.Errorf("failed to execute request: %v", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Debug.Printf("\nbody: %v\n\n", resp.Body)
		bytes, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Fatal(err)
		}

		logger.Debug.Printf("Issue from llm: %s", string(bytes))
		logger.Debug.Printf("received non-OK response status: %d", resp.StatusCode)
		logger.Debug.Printf("\nerr: %v", err)
		panic(buildHTTPStatusError(resp.StatusCode, bytes))
	}

	logger.Debug.Printf("statusCode: %d", resp.StatusCode)
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Debug.Println(err)
		println(fmt.Sprintf("Error reading response body: %v\n", err))
	}
	defer resp.Body.Close()

	logger.Debug.Println("Received a response without streaming")
	logger.Debug.Printf("bodyBytes %s", string(bodyBytes))
	if looksLikeSSEBody(bodyBytes) {
		logger.Debug.Println("awaited query returned SSE body; replaying lines through stream handler")
		for _, line := range strings.Split(string(bodyBytes), "\n") {
			model.HandleStreamedLine([]byte(line + "\n"))
		}
		return
	}

	model.HandleBodyBytes(bodyBytes)

	// Update the context's preferred model after successful query
	if context != nil && modelName != "" {
		err := historyRepository.UpdatePreferredModel(context.Id, modelName)
		if err != nil {
			logger.Debug.Printf("Failed to update preferred model: %v", err)
		}
	}
}

func StreamedQuery(prompt string, model commontypes.Model, historyRepository data.HistoryRepository, historyCount int, context *data.Context, modifiers *commontypes.PayloadModifiers, modelName string) {
	history, err := historyRepository.GetHistoryByContextId(context.Id, historyCount)
	if err != nil {
		panic(fmt.Sprintf("Could not fetch history %s", err))
	}

	logger.Screen("sending streamed query", color.RGB(150, 150, 150))

	validHistory, droppedArchived, droppedEmpty := filterStreamHistory(history)

	logger.Debug.Printf(
		"stream history filter: context=%d total=%d kept=%d dropped_archived=%d dropped_empty=%d",
		context.Id,
		len(history),
		len(validHistory),
		droppedArchived,
		droppedEmpty,
	)

	req := model.CreateRequest(context, prompt, true, validHistory, modifiers)
	if authSource := strings.TrimSpace(req.Header.Get("X-Owl-Auth-Source")); authSource != "" {
		logger.Debug.Printf("streamed query auth source: %s", authSource)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		panic(fmt.Errorf("Failed to execute request: %v", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bytes, _ := io.ReadAll(resp.Body)
		wwwAuth := strings.TrimSpace(resp.Header.Get("WWW-Authenticate"))
		if wwwAuth != "" {
			logger.Debug.Printf("streamed query non-OK WWW-Authenticate: %s", wwwAuth)
		}
		logger.Debug.Printf("streamed query non-OK status=%d body=%s", resp.StatusCode, string(bytes))

		panic(buildHTTPStatusError(resp.StatusCode, bytes))
	}

	reader := bufio.NewReader(resp.Body)
	finished := false
	for !finished {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				logger.Debug.Println("failed to read bytes from stream response")
				logger.Debug.Printf("\n%s", err)
			}
			finished = true
			continue
		}

		model.HandleStreamedLine(line)
	}

	// Update the context's preferred model after successful query
	if context != nil && modelName != "" {
		err := historyRepository.UpdatePreferredModel(context.Id, modelName)
		if err != nil {
			logger.Debug.Printf("Failed to update preferred model: %v", err)
		}
	}
}

func filterStreamHistory(history []data.History) ([]data.History, int, int) {
	validHistory := make([]data.History, 0, len(history))
	droppedArchived := 0
	droppedEmpty := 0

	for _, h := range history {
		if h.Archived {
			droppedArchived++
			continue
		}

		hasPrompt := strings.TrimSpace(h.Prompt) != ""
		hasResponse := strings.TrimSpace(h.Response) != ""
		if !hasPrompt && !hasResponse {
			droppedEmpty++
			continue
		}

		validHistory = append(validHistory, h)
	}

	return validHistory, droppedArchived, droppedEmpty
}
