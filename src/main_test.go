package main

import (
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	commontypes "owl/common_types"
	"owl/data"
	"owl/embeddings"
	server "owl/http"
	"owl/services"
)

type stubModel struct{}

func (stubModel) CreateRequest(context *data.Context, prompt string, streaming bool, history []data.History, modifiers *commontypes.PayloadModifiers) *http.Request {
	return nil
}

func (stubModel) HandleStreamedLine(line []byte) {}

func (stubModel) HandleBodyBytes(bytes []byte) {}

func (stubModel) SetResponseHandler(responseHandler commontypes.ResponseHandler) {}

func writeSkillFile(t *testing.T, name, content string) {
	t.Helper()
	dir := filepath.Join(os.Getenv("HOME"), ".owl", "skills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("failed to create skills dir: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write skill file: %v", err)
	}
}

func setupTest(t *testing.T, args []string) func() {
	t.Helper()
	origArgs := os.Args
	origFlag := flag.CommandLine
	origRunServer := runServerFunc
	origRunEmbeddings := runEmbeddingsFunc
	origAwaited := awaitedQueryFunc
	origStreamed := streamedQueryFunc
	origLaunch := launchTUIFunc
	origView := viewHistoryFunc
	origNameContext := nameNewContextFunc
	origGetContext := getContextFunc
	origGetModel := getModelForQueryFunc
	osEnv := os.Getenv("OWL_LOCAL_DATABASE")
	origHome := os.Getenv("HOME")
	tempHome := t.TempDir()
	os.Setenv("HOME", tempHome)
	os.MkdirAll(filepath.Join(tempHome, ".owl", "skills"), 0o755)
	skillsFlag = ""
	tool_groups = ""
	system_prompt = ""
	os.Args = append([]string{}, args...)
	flag.CommandLine = flag.NewFlagSet(args[0], flag.ExitOnError)
	registerFlags(flag.CommandLine)
	stream = false
	serve = false
	store = false
	view = false
	tui_mode = false
	create_context = false
	search = ""
	chunk = ""
	prompt = ""
	context_name = "misc"
	history_count = services.DefaultHistoryCount
	launchTUIFunc = launchTUI
	viewHistoryFunc = view_history
	runServerFunc = server.Run
	runEmbeddingsFunc = embeddings.Run
	awaitedQueryFunc = services.AwaitedQuery
	streamedQueryFunc = services.StreamedQuery
	nameNewContextFunc = origNameContext
	getContextFunc = origGetContext
	getModelForQueryFunc = origGetModel
	os.Setenv("OWL_LOCAL_DATABASE", "testdb")

	return func() {
		os.Args = origArgs
		flag.CommandLine = origFlag
		runServerFunc = origRunServer
		runEmbeddingsFunc = origRunEmbeddings
		awaitedQueryFunc = origAwaited
		streamedQueryFunc = origStreamed
		launchTUIFunc = origLaunch
		viewHistoryFunc = origView
		nameNewContextFunc = origNameContext
		getContextFunc = origGetContext
		getModelForQueryFunc = origGetModel
		os.Setenv("OWL_LOCAL_DATABASE", osEnv)
		os.Setenv("HOME", origHome)
	}
}

func TestMainRunsTUIWhenFlagSet(t *testing.T) {
	defer setupTest(t, []string{"cmd", "-tui"})()
	called := false
	launchTUIFunc = func() {
		called = true
	}
	main()
	if !called {
		t.Fatalf("expected launchTUI to be called")
	}
}

func TestMainServeRunsServer(t *testing.T) {
	defer setupTest(t, []string{"cmd", "-serve"})()
	called := false
	runServerFunc = func(sec bool, p int, handler *server.HttpResponseHandler, model commontypes.Model, streaming bool) {
		called = true
		if sec {
			t.Fatalf("secure should default false")
		}
	}
	main()
	if !called {
		t.Fatalf("expected server to run")
	}
}

func TestMainEmbeddingsStore(t *testing.T) {
	defer setupTest(t, []string{"cmd", "-embeddings", "-chunk", "doc.md"})()
	called := false
	runEmbeddingsFunc = func(cfg embeddings.Config) ([]data.EmbeddingMatch, error) {
		called = true
		if !cfg.Store {
			t.Fatalf("expected store flag")
		}
		if cfg.ChunkPath != "doc.md" {
			t.Fatalf("unexpected chunk path %s", cfg.ChunkPath)
		}
		return nil, nil
	}
	main()
	if !called {
		t.Fatalf("expected embeddings.Run to be called")
	}
}

func TestMainSearchUsesAwaitedQuery(t *testing.T) {
	defer setupTest(t, []string{"cmd", "-search", "foo"})()
	runEmbeddingsFunc = func(cfg embeddings.Config) ([]data.EmbeddingMatch, error) {
		return []data.EmbeddingMatch{{Text: "match", Reference: "ref", Distance: 0.1}}, nil
	}
	getContextFunc = func(repo data.HistoryRepository, systemPrompt *string) *data.Context {
		return &data.Context{Id: 1, Name: "ctx"}
	}
	getModelForQueryFunc = func(model string, context *data.Context, handler commontypes.ResponseHandler, repository data.HistoryRepository, stream bool, thinking bool, streamThinking bool, outputThinking bool) (commontypes.Model, string) {
		return stubModel{}, "stub"
	}
	captured := ""
	capturedHistory := 0
	awaitedQueryFunc = func(prompt string, model commontypes.Model, historyRepository data.HistoryRepository, historyCount int, context *data.Context, modifiers *commontypes.PayloadModifiers, modelName string) {
		captured = prompt
		capturedHistory = historyCount
	}
	main()
	if !strings.Contains(captured, "Matches from RAG") {
		t.Fatalf("expected prompt to include RAG info, got %s", captured)
	}
	if capturedHistory != services.DefaultHistoryCount {
		t.Fatalf("expected default history count, got %d", capturedHistory)
	}
}

func TestMainDefaultUsesAwaitedQuery(t *testing.T) {
	defer setupTest(t, []string{"cmd", "-prompt", "hello"})()
	getContextFunc = func(repo data.HistoryRepository, systemPrompt *string) *data.Context {
		return &data.Context{Id: 42, Name: "ctx"}
	}
	getModelForQueryFunc = func(model string, context *data.Context, handler commontypes.ResponseHandler, repository data.HistoryRepository, stream bool, thinking bool, streamThinking bool, outputThinking bool) (commontypes.Model, string) {
		return stubModel{}, "stub"
	}
	called := false
	awaitedQueryFunc = func(prompt string, model commontypes.Model, historyRepository data.HistoryRepository, historyCount int, context *data.Context, modifiers *commontypes.PayloadModifiers, modelName string) {
		called = true
		if prompt != "hello" {
			t.Fatalf("unexpected prompt %s", prompt)
		}
		if historyCount != services.DefaultHistoryCount {
			t.Fatalf("expected default history count, got %d", historyCount)
		}
	}
	streamedQueryFunc = func(prompt string, model commontypes.Model, historyRepository data.HistoryRepository, historyCount int, context *data.Context, modifiers *commontypes.PayloadModifiers, modelName string) {
		t.Fatalf("streamed query should not run")
	}
	main()
	if !called {
		t.Fatalf("expected awaited query to run")
	}
}

func TestMainStreamFlagUsesStreamedQuery(t *testing.T) {
	defer setupTest(t, []string{"cmd", "-prompt", "hi", "-stream"})()
	getContextFunc = func(repo data.HistoryRepository, systemPrompt *string) *data.Context {
		return &data.Context{Id: 1}
	}
	getModelForQueryFunc = func(model string, context *data.Context, handler commontypes.ResponseHandler, repository data.HistoryRepository, stream bool, thinking bool, streamThinking bool, outputThinking bool) (commontypes.Model, string) {
		return stubModel{}, "stub"
	}
	called := false
	streamedQueryFunc = func(prompt string, model commontypes.Model, historyRepository data.HistoryRepository, historyCount int, context *data.Context, modifiers *commontypes.PayloadModifiers, modelName string) {
		called = true
	}
	main()
	if !called {
		t.Fatalf("expected streamed query to run")
	}
}

func TestMainDefaultsToCodexWhenOAuthPresentAndModelNotProvided(t *testing.T) {
	defer setupTest(t, []string{"cmd", "-prompt", "hi"})()

	home := os.Getenv("HOME")
	authPath := filepath.Join(home, ".owl", "auth", "openai.json")
	if err := os.MkdirAll(filepath.Dir(authPath), 0o700); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	content := []byte(`{"type":"oauth","access_token":"token","refresh_token":"refresh","expires_at":9999999999999}`)
	if err := os.WriteFile(authPath, content, 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	getContextFunc = func(repo data.HistoryRepository, systemPrompt *string) *data.Context {
		return &data.Context{Id: 1, Name: "ctx"}
	}

	resolvedModel := ""
	getModelForQueryFunc = func(model string, context *data.Context, handler commontypes.ResponseHandler, repository data.HistoryRepository, stream bool, thinking bool, streamThinking bool, outputThinking bool) (commontypes.Model, string) {
		resolvedModel = model
		return stubModel{}, "stub"
	}

	awaitedQueryFunc = func(prompt string, model commontypes.Model, historyRepository data.HistoryRepository, historyCount int, context *data.Context, modifiers *commontypes.PayloadModifiers, modelName string) {
	}

	main()

	if resolvedModel != "codex" {
		t.Fatalf("expected default requested model codex, got %q", resolvedModel)
	}
}

func TestMainViewFlag(t *testing.T) {
	defer setupTest(t, []string{"cmd", "-view", "-context_name", "ctx"})()
	called := false
	viewHistoryFunc = func() {
		called = true
	}
	main()
	if !called {
		t.Fatalf("expected view history to run")
	}
}

func TestSkillsAppliedToContextAndAwaited(t *testing.T) {
	defer setupTest(t, []string{"cmd", "-prompt", "hello", "--skills=poem"})()
	writeSkillFile(t, "poem.md", "Use rhymes")
	getContextFunc = func(repo data.HistoryRepository, systemPrompt *string) *data.Context {
		if systemPrompt == nil || !strings.Contains(*systemPrompt, "Use rhymes") {
			t.Fatalf("expected skills in system prompt input")
		}
		return &data.Context{Id: 5, Name: "ctx"}
	}
	getModelForQueryFunc = func(model string, context *data.Context, handler commontypes.ResponseHandler, repository data.HistoryRepository, stream bool, thinking bool, streamThinking bool, outputThinking bool) (commontypes.Model, string) {
		return stubModel{}, "stub"
	}
	capturedContextPrompt := ""
	awaitedQueryFunc = func(prompt string, model commontypes.Model, historyRepository data.HistoryRepository, historyCount int, context *data.Context, modifiers *commontypes.PayloadModifiers, modelName string) {
		capturedContextPrompt = context.SystemPrompt
	}
	main()
	if !strings.Contains(capturedContextPrompt, "Use rhymes") {
		t.Fatalf("expected context system prompt to include skills, got %s", capturedContextPrompt)
	}
}
