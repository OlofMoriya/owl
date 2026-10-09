# Owl Application Architecture

## Overview

Owl is a terminal-based AI chat application written in Go that provides multiple interfaces for interacting with various Large Language Models (LLMs). The application supports multiple AI providers (Claude, OpenAI, Grok, Ollama), features a rich tool system for file operations and integrations, and includes conversation history management with embeddings support for RAG (Retrieval Augmented Generation).

### Key Features

- **Multi-Model Support**: Claude (Opus, Sonnet, Haiku), OpenAI (GPT-4, GPT-4o), Grok, and Ollama
- **Multiple Interfaces**: Command-line, TUI (Terminal User Interface), and HTTP API server
- **Tool System**: Extensible tool framework with 10+ built-in tools (file operations, git, HTTP requests, etc.)
- **Conversation Management**: SQLite-based storage with context management and history tracking
- **Embeddings & RAG**: Vector search capabilities using SQLite-vec or DuckDB for semantic search
- **Streaming Support**: Real-time streaming responses from AI models
- **Multi-User Support**: Separate databases per user in server mode

### Core Architecture

The application follows a modular architecture organized into several key packages:

1. **models/** - AI model implementations and abstractions
2. **tools/** - Tool system for extending AI capabilities
3. **data/** - Database operations and data models
4. **services/** - Business logic and utility services
5. **tui/** - Terminal User Interface implementation
6. **embeddings/** - Vector embeddings for RAG
7. **http/** - HTTP server for remote API access
8. **logger/** - Logging and status messaging
9. **mode/** - Application mode management (LOCAL vs REMOTE)

---

## Owl architecture - main.go

**Purpose**: Main entry point and command-line argument handling

This file orchestrates the entire application lifecycle. It:
- Parses command-line flags for all application modes
- Initializes the logger
- Routes execution to appropriate modes (CLI, TUI, Server, Embeddings, View)
- Delegates model selection to `picker.GetModelForQuery`
- Manages context creation and system prompt configuration
- Handles history viewing with markdown rendering

**Key Functions**:
- `main()` - Entry point, routes to different modes
- `picker.GetModelForQuery()` - Instantiates models based on flags/context
- `view_history()` - Displays conversation history with glamour markdown rendering
- `launchTUI()` - Initializes and starts the TUI mode

---

## Owl architecture - get-context.go

**Purpose**: Context retrieval and creation helper

Simple utility that retrieves or creates a conversation context by name. Contexts are used to organize separate conversations with their own history and system prompts.

**Key Functions**:
- `getContext()` - Gets existing context or creates new one with given name and system prompt

---

## Owl architecture - cli-response-handler.go

**Purpose**: Response handler for CLI mode

Implements the `ResponseHandler` interface for command-line usage. Handles:
- Streaming text output with optional color formatting
- Saving responses to database with tool results
- Extracting and copying code blocks to clipboard
- Rendering markdown responses using glamour

**Type**: `CliResponseHandler`

---

## Owl architecture - secrets.go

**Purpose**: Google Cloud Secret Manager integration

Provides functionality to fetch secrets from Google Cloud Platform's Secret Manager. Used for retrieving API keys and credentials in production environments.

**Key Functions**:
- `GetSecretFromGCP()` - Fetches a secret value from GCP Secret Manager

---

## Owl architecture - go.mod

**Purpose**: Go module definition and dependency management

Defines the module path (`owl`) and all required dependencies including:
- Bubbletea/Bubbles for TUI
- Multiple database drivers (SQLite, PostgreSQL, DuckDB)
- AI provider clients
- Vector search libraries (sqlite-vec)
- Google Cloud libraries

---

# Tools Package

The tools package implements an extensible tool system that allows AI models to perform actions like file operations, HTTP requests, and git commands. All tools implement the `ToolModel` interface and register themselves for automatic discovery.

## Owl architecture - tools/model.go

**Purpose**: Tool data structures and schema definitions

Defines the core data structures for the tool system:
- `Tool` - Tool definition with name, description, and input schema
- `InputSchema` - JSON schema for tool parameters
- `Property` - Individual parameter definitions

These structures are used to describe tools to AI models in a standardized format.

---

## Owl architecture - tools/tool_runner.go

**Purpose**: Tool execution engine and registry

The heart of the tool system. Implements:
- Global tool registry using a thread-safe map
- Tool registration via `Register()`
- Tool execution via `ExecuteTool()`
- Mode-based tool filtering (LOCAL vs REMOTE)
- Tool discovery for AI models

**Key Types**:
- `ToolModel` - Interface all tools must implement
- `ToolRunner` - Executes tools with context and history
- `ToolRegistry` - Thread-safe tool storage

---

## Owl architecture - tools/tool_response_handler.go

**Purpose**: Response handler for tool executions

Wrapper around the standard ResponseHandler used when AI models execute tools internally. Provides a channel-based interface for asynchronous tool execution.

**Type**: `ToolResponseHandler`

---

## Owl architecture - tools/git_tool.go

**Purpose**: Git repository information tool

Executes git commands to provide repository context to AI models. Supports:
- `status` - Show changed files
- `branch` - Current branch name
- `log` - Recent commits
- `diff` - Changes summary
- `uncommitted` - Full diff of uncommitted changes

**Tool Name**: `git_info`

---

## Owl architecture - tools/read_file_tool.go

**Purpose**: File reading tool

Reads one or more files and returns their contents. Supports multiple files separated by semicolons. Preferred file extensions include: .go, .md, .tsx, .ts, .csv, .js, .txt, .mod, .cs, .csproj, .gitignore, .jsx, .json

**Tool Name**: `read_file`

---

## Owl architecture - tools/write_file_tool.go

**Purpose**: File creation/overwrite tool

Creates new files or overwrites existing ones with provided content. Includes security checks to prevent:
- Parent directory traversal (..)
- Root directory access (/)
- Home directory access (~)

**Tool Name**: `write_file`

---

## Owl architecture - tools/update_file_tool.go

**Purpose**: Surgical file modification tool

Applies targeted changes to existing files using Git-style unified diffs.

Includes an optional approval system. When Owl's TUI is active, the tool sends a request through `interaction.DiffApprovalPromptChan`; the existing chat program renders the conversation and diff together and returns the decision without starting a nested Bubble Tea program. CLI runs retain standalone Bubble Tea, tmux, and terminal fallbacks.

**Tool Name**: `update_file`

**Key Features**:
- Diff preview generation with a required user-facing change summary
- Responsive side-by-side or stacked review workspace in chat
- User approval workflow over request/response channels
- Standalone viewer fallbacks outside the integrated TUI
- Safety checks (same as write_file)

---

## Owl architecture - tools/list_files_tool.go

**Purpose**: Directory listing tool

Lists all files in the current directory and subdirectories. Excludes `.git` and `node_modules`. Used by AI to understand project structure.

**Tool Name**: `list_files`

**Implementation**: Uses `find` command with pruning

---

## Owl architecture - tools/todo_tool.go

**Purpose**: Todo item creation tool

Creates todo items using the external `todo-tui` application. Supports:
- Title (required)
- Description (optional)
- Due date in days from today (optional)

**Tool Name**: `create_todo`

**Integration**: Calls `todo-tui` CLI with appropriate arguments

---

## Owl architecture - tools/calendar_overview_tool.go

**Purpose**: Read-only calendar/task overview via icalBuddy

Provides calendar and task visibility using local `icalBuddy` command execution. Supports event windows, active events, incomplete tasks, and calendar listing while blocking config/edit commands.

**Tool Name**: `calendar_overview`

**Groups**: manager, secretary

---


## Owl architecture - tools/generate_image_tool.go

**Purpose**: AI image generation tool

Generates images from text prompts using OpenAI's image generation API. Returns base64-encoded images and saves them as PNG files.

**Tool Name**: `image_generator`

**Note**: Time-intensive, should be limited to 1-2 generations per request

---


## Owl architecture - tools/http_request_tool.go

**Purpose**: HTTP request execution tool

Makes HTTP requests to external APIs. Supports:
- Methods: GET, POST, PUT, DELETE, PATCH
- Custom headers (semicolon-separated)
- Request body (typically JSON)
- 30-second timeout

**Tool Name**: `http_request`

**Status**: Currently commented out in init() - not active by default

---

## Owl architecture - tools/fetch_url_content_tool.go

**Purpose**: Website content extraction tool

Fetches HTML pages over HTTP(S), extracts the main readable content, and returns markdown (or plain text) instead of raw full-page HTML. Enforces URL scheme validation, response size limits, and output truncation controls.

**Tool Name**: `fetch_url_content`

**Groups**: planner, developer, manager, secretary

---

## Owl architecture - tools/diff_viewer.go

**Purpose**: Standalone interactive diff approval viewer

Bubbletea-based fallback for reviewing and approving file changes outside Owl's integrated chat TUI. Features:
- Syntax-highlighted diff display
- Scrollable viewport
- Approve/Reject/Cancel options
- Keyboard navigation

**Key Types**:
- `DiffViewerModel` - Bubbletea model
- `DiffApprovalResult` - User decision enum
- `ShowDiffForApproval()` - Main entry point

---

## Owl architecture - tools/diff_viewer_tmux.go

**Purpose**: TMUX-based diff viewer (implementation details not in files read)

Alternative diff viewer that uses TMUX for displaying changes. Used when TMUX is available.

---

# Models Package

The models package contains implementations for various AI providers. Each model implements the `Model` interface and handles API communication, request formatting, and response parsing.

## Owl architecture - models/model.go

**Purpose**: Core model interfaces and types

Defines:
- `Model` interface - All AI models must implement this
- `PayloadModifiers` - Additional request modifiers (PDF, Web, Image, ToolResponses)
- `ToolResponse` - Results from tool executions

**Key Interface Methods**:
- `CreateRequest()` - Build HTTP request for the model
- `HandleStreamedLine()` - Process streaming response chunks
- `HandleBodyBytes()` - Process complete response body
- `SetResponseHandler()` - Configure output handling

---

## Owl architecture - models/response-handler.go

**Purpose**: Response handler interface

Defines the `ResponseHandler` interface used by all models to output text. Implementations include CLI, TUI, HTTP, and tool handlers.

**Interface Methods**:
- `RecievedText()` - Handle incremental text (streaming)
- `FinalText()` - Handle complete response with metadata

---

## Owl architecture - picker/model_picker.go

**Purpose**: Centralized model selection

Contains `picker.GetModelForQuery`, the single entry point for instantiating AI models. It:
- Applies context preferences and CLI/TUI flags to choose a provider
- Configures provider-specific structs (Claude, OpenAI, Grok, Ollama, Gemini)
- Ensures consistent streaming/thinking parameters across modes

When adding a new provider, wire it in here so CLI, TUI, and HTTP layers all gain support automatically.

---

## Owl architecture - models/claude/claude-model.go

**Purpose**: Claude (Anthropic) model implementation

Comprehensive implementation for Claude models (Opus, Sonnet, Haiku). Features:
- Streaming and non-streaming support
- Extended thinking mode
- Tool use handling
- Prompt caching for cost optimization
- Web search integration
- Multimodal support (images, PDFs)

**Key Type**: `ClaudeModel`

**Key Features**:
- Version selection (opus/sonnet/haiku)
- Tool execution and response handling
- Cache control for system prompts and history
- Streaming tool use accumulation

---

## Owl architecture - models/claude/claude-data-model.go

**Purpose**: Claude API data structures (implementation details not in files read)

Contains request/response structures for Claude API including message formats, tool definitions, and thinking blocks.

---

## Owl architecture - models/ollama/ollama-model.go

**Purpose**: Ollama local model implementation (implementation details not in files read)

Implements support for locally-hosted Ollama models like Qwen3.

---

## Owl architecture - models/ollama/ollama-data-model.go

**Purpose**: Ollama API data structures (implementation details not in files read)

---

## Owl architecture - models/grok/grok-model.go

**Purpose**: Grok (xAI) model implementation (implementation details not in files read)

Uses OpenAI-compatible API structure.

---

## Owl architecture - models/grok/grok-data-model.go

**Purpose**: Grok API data structures (implementation details not in files read)

---

## Owl architecture - models/open-ai-gpt/openai-gpt-model.go

**Purpose**: OpenAI GPT model implementation (implementation details not in files read)

Supports GPT-4 and Codex models.

---

## Owl architecture - models/open-ai-gpt/openai-gpt-data-model.go

**Purpose**: OpenAI GPT API data structures (implementation details not in files read)

---

## Owl architecture - models/open-ai-4o/open-ai-o-model.go

**Purpose**: OpenAI GPT-4o model implementation (implementation details not in files read)

Supports the multimodal GPT-4o model.

---

## Owl architecture - models/open-ai-4o/open-ai-o-data-model.go

**Purpose**: OpenAI GPT-4o API data structures (implementation details not in files read)

---

## Owl architecture - models/open-ai-base/openai-base-model.go

**Purpose**: Base OpenAI-compatible model (implementation details not in files read)

Shared functionality for OpenAI-compatible APIs (Grok, etc).

---

## Owl architecture - models/open-ai-base/openai-base-data-model.go

**Purpose**: Base OpenAI API data structures (implementation details not in files read)

---

## Owl architecture - models/open-ai-vision/openai-vision-model.go

**Purpose**: OpenAI Vision model implementation (implementation details not in files read)

Handles image input for vision tasks.

---

## Owl architecture - models/open-ai-vision/openai-vision-data-model.go

**Purpose**: OpenAI Vision API data structures (implementation details not in files read)

---

## Owl architecture - models/open-ai-responses/open-ai-responses-model.go

**Purpose**: OpenAI DALL-E image generation (implementation details not in files read)

Used by the image generation tool.

---

## Owl architecture - models/open-ai-responses/open-ai-responses-data-model.go

**Purpose**: OpenAI image response data structures (implementation details not in files read)

---

## Owl architecture - models/open-ai-embedings/embeddings-model.go

**Purpose**: OpenAI embeddings model (implementation details not in files read)

Generates vector embeddings for RAG.

---

## Owl architecture - models/open-ai-embedings/embeddings-data-model.go

**Purpose**: OpenAI embeddings API data structures (implementation details not in files read)

---

## Owl architecture - models/vertex-claude/vertex-claude-model.go

**Purpose**: Google Vertex AI Claude implementation (implementation details not in files read)

Alternative Claude access through Google Cloud.

---

## Owl architecture - models/vertex-claude/claude-data-model.go

**Purpose**: Vertex Claude API data structures (implementation details not in files read)

---

# Data Package

The data package handles all database operations, including conversation history, context management, user data, and vector embeddings storage.

## Owl architecture - data/history-repository.go

**Purpose**: Repository interface for conversation storage

Defines the `HistoryRepository` interface that all storage implementations must provide. This abstraction allows multiple backends (single-user SQLite, multi-user, PostgreSQL).

**Interface Methods**:
- Context operations: `GetContextById`, `InsertContext`, `GetContextByName`, `GetAllContexts`, `DeleteContext`
- History operations: `InsertHistory`, `GetHistoryByContextId`, `DeleteHistory`
- Settings: `UpdateSystemPrompt`, `UpdatePreferredModel`

---

## Owl architecture - data/history-model.go

**Purpose**: Data models for conversations

Defines the core data structures:
- `Context` - Conversation context with name, system prompt, preferred model
- `History` - Individual message exchange with prompt, response, metadata

These are used throughout the application for storing and retrieving conversations.

---

## Owl architecture - data/user-model.go

**Purpose**: User data model

Simple user structure with ID, Name, Email, and SlackId. Used in multi-user scenarios.

**Type**: `User`

---

## Owl architecture - data/sqllite-db.go

**Purpose**: Single-user SQLite implementation

Implements `HistoryRepository` for single-user CLI/TUI usage. Features:
- User-specific database files in `~/.owl/`
- Automatic schema creation
- Context and history table management
- CRUD operations for contexts and history

**Type**: `User` (with methods implementing HistoryRepository)

**Database Location**: `~/.owl/{username}.db`

---

## Owl architecture - data/multi-user-sqllite-db.go

**Purpose**: Multi-user SQLite wrapper

Wrapper around single-user implementation for HTTP server mode. Allows switching between user databases based on authenticated username.

**Type**: `MultiUserContext`

**Key Method**: `SetCurrentDb()` - Switches active user database

---

## Owl architecture - data/postgres-db.go

**Purpose**: PostgreSQL implementation (implementation details not in files read)

Alternative storage backend using PostgreSQL for larger deployments.

---

## Owl architecture - data/embeddings-db.go

**Purpose**: Embeddings storage interface

Defines the `EmbeddingsStore` interface for vector embeddings:
- `FindMatches()` - Semantic search for similar embeddings
- `InsertEmbedding()` - Store text with its vector embedding

**Type**: `EmbeddingMatch` - Search result with text, distance, and reference

---

## Owl architecture - data/embeddings-db-sqllite.go

**Purpose**: SQLite vector embeddings storage

SQLite implementation using sqlite-vec extension for vector search. Stores:
- `texts` table - Original text content and references
- `embeddings` virtual table - 1536-dimension vectors

Uses sqlite-vec's MATCH operator for similarity search.

**Type**: `EmbeddingsDatabase`

---

## Owl architecture - data/embeddings-db-duckdb.go

**Purpose**: DuckDB vector embeddings storage

DuckDB implementation using VSS (Vector Similarity Search) extension. Provides:
- Better performance for large vector datasets
- Native FLOAT[] array support
- VSS indexing for fast similarity search

**Type**: `DuckDbEmbeddingsDatabase`

**Key Difference**: Uses DuckDB's vss_match() function instead of SQLite's MATCH operator

---

## Owl architecture - data/utils.go

**Purpose**: Data package utilities

Helper functions for data operations.

**Key Functions**:
- `getHomeDir()` - Gets user home directory for database storage

---

# Services Package

The services package contains business logic and utility functions used across the application.

## Owl architecture - services/query.go

**Purpose**: AI model query execution

Core query execution functions for both streaming and non-streaming requests. Handles:
- History loading and validation
- HTTP request execution
- Response body reading
- Model version tracking and updates

**Key Functions**:
- `AwaitedQuery()` - Execute blocking query
- `StreamedQuery()` - Execute streaming query with real-time output

---

## Owl architecture - services/chunking.go

**Purpose**: Document chunking for embeddings

Intelligent text chunking optimized for RAG with 1536-dimension embeddings. Features:
- Semantic splitting on markdown headers
- Size-aware chunking (4000 char optimal, 8000 max)
- Paragraph and sentence boundary detection
- Header preservation in chunks

**Key Functions**:
- `ChunkMarkdown()` - Main chunking function for markdown
- `ChunkText()` - Simpler chunking for plain text

**Constants**:
- `OptimalChunkSize` = 4000 chars (~1000 tokens)
- `MaxChunkSize` = 8000 chars (~2000 tokens)
- `MinChunkSize` = 500 chars

---

## Owl architecture - services/chunking_test.go

**Purpose**: Chunking algorithm tests (implementation details not in files read)

Unit tests for the chunking functionality.

---

## Owl architecture - services/pdf.go

**Purpose**: PDF file handling

Reads PDF files and converts them to base64 for sending to AI models. Includes validation for file existence and PDF format.

**Key Functions**:
- `ReadPDFAsBase64()` - Read PDF and return base64 string

---

## Owl architecture - services/clipboard.go

**Purpose**: Clipboard operations

Cross-platform clipboard access for images. Features:
- Image extraction from clipboard
- PNG encoding
- Base64 conversion
- File saving with timestamps

**Key Functions**:
- `GetImageFromClipboard()` - Extract image from system clipboard
- `ImageToBase64()` - Convert image to base64 string
- `saveClipboardImageAsPng()` - Save clipboard image to file

**Platform**: macOS implementation using osascript

---

## Owl architecture - services/extract_code.go

**Purpose**: Code block extraction

Extracts code blocks from markdown responses for clipboard copying. Uses regex to find fenced code blocks (```...```).

**Key Functions**:
- `ExtractCodeBlocks()` - Extract all code blocks from markdown

---

# TUI Package

The TUI package implements a full-featured Terminal User Interface using Bubbletea.

## Owl architecture - tui/tui.go

**Purpose**: TUI initialization and configuration

Main entry point for TUI mode. Configures the Bubbletea program and sets up the status channel for logger integration.

**Key Functions**:
- `Run()` - Start the TUI application
- `initialModel()` - Create initial view (list view)

**Type**: `TUIConfig` - Configuration for TUI with repository, model, and history count

---

## Owl architecture - tui/models.go

**Purpose**: TUI shared state and types

Defines shared types used across TUI views:
- `viewMode` - Enum for different views (list, chat)
- `contextItem` - Context with message count
- `sharedState` - State shared between views

---

## Owl architecture - tui/list_view.go

**Purpose**: Context list view (implementation details not in files read)

Shows all available conversation contexts. Allows selecting, creating, and deleting contexts.

---

## Owl architecture - tui/chat_view.go

**Purpose**: Chat conversation view

Interactive chat interface with:
- Message history display with markdown rendering
- Text input area
- Model selection menu
- Normal and Input modes (vim-like)
- Scrolling and navigation
- History count adjustment

**Type**: `chatViewModel`

**Key Features**:
- Dual mode: chatInputMode and chatNormalMode
- Model switching (Ctrl+G)
- History view access (Ctrl+H)
- Streaming response display
- Code block extraction to clipboard

---

## Owl architecture - tui/chat_histoy_view.go

**Purpose**: Chat history view (implementation details not in files read)

Detailed view of conversation history with search and navigation.

---

## Owl architecture - tui/styles.go

**Purpose**: TUI styling definitions (implementation details not in files read)

Lipgloss style definitions for consistent TUI appearance.

---

# Embeddings Package

The embeddings package handles vector embeddings generation and RAG functionality.

## Owl architecture - embeddings/embeddings.go

**Purpose**: Embeddings orchestration

High-level embeddings API that coordinates:
- Backend selection (SQLite or DuckDB)
- Document chunking and embedding
- Semantic search
- Single text embedding

**Key Functions**:
- `Run()` - Main entry point for embeddings operations

**Type**: `Config` - Embeddings configuration with backend, store mode, paths, and queries

**Supported Operations**:
1. Chunk and store a markdown document
2. Search for similar text
3. Generate single embedding

---

## Owl architecture - embeddings/response_handler.go

**Purpose**: Embeddings response handler

Handles embedding responses from the OpenAI embeddings model. Can either:
- Store embeddings in the database (when Store=true)
- Return embeddings on a channel (when Store=false, for search)

**Type**: `ResponseHandler`

---

# HTTP Package

The HTTP package provides a REST API server for remote access to Owl.

## Owl architecture - http/server.go

**Purpose**: HTTP API server

Full-featured HTTP server with:
- JWT-based authentication
- Context management endpoints
- Prompt submission with streaming
- Multi-user support
- CORS handling

**Key Endpoints**:
- `POST /api/login` - Authenticate and get JWT
- `GET /api/context` - List all contexts
- `GET /api/context/{id}` - Get context with history
- `POST /api/prompt` - Submit prompt and get response
- `POST /api/context/{id}/systemprompt` - Set system prompt
- `POST /api/context/{id}/setmodel` - Set preferred model
- `GET /status` - Health check

**Key Functions**:
- `Run()` - Start HTTP server
- `picker.GetModelForQuery()` - Model selection shared with CLI/TUI
- `name_new_context()` - Auto-generate context names using AI

**Type**: `HttpResponseHandler` - Response handler for HTTP streaming

---

# Logger Package

## Owl architecture - logger/logger.go

**Purpose**: Logging and status output

Dual-mode logger that:
- Writes debug logs to file (`~/.owl/debug.log`)
- Outputs to console in CLI mode
- Sends messages to TUI via channel in TUI mode

**Key Functions**:
- `Init()` - Initialize logger with file path
- `Screen()` - Output to screen or TUI status channel

**Global Variables**:
- `Debug` - File logger instance
- `StatusChan` - Channel for TUI status messages

---

# Mode Package

## Owl architecture - mode/mode.go

**Purpose**: Application mode tracking

Simple package that tracks whether the application is in LOCAL (CLI/TUI) or REMOTE (server) mode. Used by the tool system to determine which tools are available.

**Global Variable**: `Mode` - Current application mode (LOCAL or REMOTE)

---

# Additional Documentation Files

The project includes several markdown documentation files:

- **SKILLS.md** - Comprehensive user guide with all features, tools, and usage examples
- **README.md** - Project overview and quick start (not read in this session)
- **CHUNKING_IMPLEMENTATION.md** - Details on chunking algorithm
- **QUICK_START_CHUNKING.md** - Quick guide for embeddings
- **BEFORE_AFTER_CHUNKING.md** - Chunking examples
- **claude-caching-implementation-plan.md** - Prompt caching implementation notes
- **grok-web-search-fix-summary.md** - Web search feature notes

---

# Key Design Patterns

## Interface-Based Architecture

The application heavily uses interfaces for extensibility:
- `Model` interface - All AI providers implement this
- `ResponseHandler` interface - Multiple output handlers
- `HistoryRepository` interface - Swappable storage backends
- `EmbeddingsStore` interface - Multiple vector databases
- `ToolModel` interface - Extensible tool system

## Tool Registration Pattern

Tools self-register using init() functions:
```go
func init() {
    Register(&MyTool{})
}
```

This allows automatic discovery without central configuration.

## Mode-Based Tool Filtering

Tools declare their availability (LOCAL or REMOTE), allowing the server to expose only appropriate tools.

## Streaming Architecture

Supports both streaming and non-streaming responses throughout:
- Models handle both via `HandleStreamedLine()` and `HandleBodyBytes()`
- Response handlers support incremental text
- TUI uses channels for async updates

## Multi-Backend Storage

Abstraction layers allow swapping:
- User databases: Single-user vs multi-user SQLite
- Embeddings: SQLite-vec vs DuckDB
- Future: PostgreSQL support

---

# Development Guidelines

## Adding a New Tool

1. Create file in `tools/` package
2. Implement `ToolModel` interface
3. Add `init()` function to register tool
4. Set mode (LOCAL or REMOTE)
5. Define input schema

## Adding a New AI Model

1. Create subdirectory in `models/`
2. Implement `Model` interface
3. Handle both streaming and non-streaming
4. Register it in `picker/model_picker.go` so CLI, TUI, and HTTP all see it

## Adding a New Storage Backend

1. Implement `HistoryRepository` interface
2. Implement `EmbeddingsStore` interface if needed
3. Add selection logic in main.go or embeddings/embeddings.go

---

# Database Schema

## User Database (SQLite)

### contexts table
- id (INTEGER PRIMARY KEY AUTOINCREMENT)
- name (TEXT)
- system_prompt (TEXT)
- preferred_model (TEXT)

### history table
- id (INTEGER PRIMARY KEY AUTOINCREMENT)
- context_id (INTEGER)
- prompt (TEXT)
- response (TEXT)
- response_content (TEXT) - JSON of structured response
- abreviation (TEXT)
- token_count (INTEGER)
- created (INT)
- tool_results (TEXT) - JSON of tool execution results
- model (TEXT)

## Embeddings Database

### texts table (SQLite/DuckDB)
- id (INTEGER/BIGINT PRIMARY KEY)
- content (TEXT)
- reference (TEXT) - Source file or reference

### embeddings table
- **SQLite**: Virtual table using vec0 with float[1536]
- **DuckDB**: Regular table with FLOAT[1536] and VSS index

---

# Configuration

## Environment Variables

- `ANTHROPIC_API_KEY` / `CLAUDE_API_KEY` - Claude API key
- `OPENAI_API_KEY` - OpenAI API key
- `GROK_API_KEY` - Grok API key
- `OLLAMA_HOST` - Ollama server URL
- `OWL_LOCAL_DATABASE` - Database name (default: "owl")
- `OWL_LOCAL_EMBEDDINGS_DATABASE` - Embeddings DB name (default: "owl_embeddings")
- `OWL_REQUIRE_APPROVAL` - Enable diff approval for file updates
- `GOOGLE_CLOUD_PROJECT` - GCP project for secret manager
- `GOOGLE_APPLICATION_CREDENTIALS` - GCP credentials file

## Command-Line Flags

See SKILLS.md for complete flag reference. Key flags:
- `-prompt` - Direct prompt text
- `-model` - AI model selection
- `-tui` - Launch TUI mode
- `-serve` - HTTP server mode
- `-embeddings` - Enable embeddings generation
- `-search` - Semantic search query

---

# File Organization Best Practices

1. **Model implementations** go in `models/{provider}/`
2. **Tool implementations** go in `tools/` as single files
3. **Data models** stay in `data/`
4. **Business logic** goes in `services/`
5. **UI components** go in `tui/`
6. **Configuration and initialization** in root

This architecture enables:
- Easy addition of new AI models
- Simple tool creation and registration
- Swappable storage backends
- Multiple UI modes from same core
- Clean separation of concerns
