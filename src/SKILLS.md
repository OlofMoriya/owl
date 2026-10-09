# Owl Application Skills & Tools

This document outlines all available skills and tools that the Owl TUI chat application can utilize.

## Overview

Owl is a terminal-based chat application that integrates with multiple AI models and provides a suite of tools for file manipulation, git operations, HTTP requests, and more.

---

## Quick Start & Installation

### Prerequisites
- Go 1.24.0 or higher
- Git (for version control operations)
- API keys for AI models you want to use (Claude, OpenAI, Grok, etc.)

### Environment Setup

Create a `.env` file in the project root with your API credentials:

```bash
ANTHROPIC_API_KEY=your_claude_key
OPENAI_API_KEY=your_openai_key
OWL_LOCAL_DATABASE=owl  # Optional: database name (default: owl)
```

### Building & Running

```bash
# Clone and setup
git clone <repository>
cd owl
go mod download

# Build the application
go build -o owl

# Run the application
./owl [flags]
```

---

## Command Line Arguments & Usage

### Basic Usage Modes

#### 1. Interactive Prompt Mode (Default)
```bash
# Launch with a direct prompt
./owl -prompt "What is Go?"

# Launch and wait for input
./owl
# Then type your prompt when prompted
```

#### 2. TUI Mode (Terminal User Interface)
```bash
./owl -tui
```
Launch the interactive terminal interface with chat history, conversation management, and context switching. This is the recommended way to use Owl for extended conversations.

**TUI Features:**
- Interactive chat interface
- Conversation history viewing
- Context switching
- Message formatting and markdown support
- Real-time streaming responses

#### 3. Server Mode (HTTP API)
```bash
# Basic server mode
./owl -serve

# With custom port
./owl -serve -port 8080

# With HTTPS enabled
./owl -serve -port 8443 -secure
```
Launches Owl as an HTTP API server for programmatic access.

---

## Complete Flag Reference

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-prompt` | string | "" | Direct prompt text to send to the model |
| `-context_name` | string | "misc" | Named context for organizing conversations |
| `-history` | int | 1 | Number of previous messages to include in context |
| `-model` | string | "claude" | AI model to use (see Model Selection below) |
| `-tui` | bool | false | Launch interactive TUI mode |
| `-serve` | bool | false | Enable HTTP server mode |
| `-port` | int | 3000 | Port for HTTP server |
| `-secure` | bool | false | Enable HTTPS for server mode |
| `-stream` | bool | false | Enable streaming responses |
| `-embeddings` | bool | false | Enable embeddings generation and storage |
| `-store_backend` | string | "duckdb" | Backend for embeddings: `sqlite` or `duckdb` |
| `-search` | string | "" | Search embeddings for a specific phrase |
| `-system` | string | "" | Custom system prompt for the context |
| `-thinking` | bool | true | Enable extended thinking mode (Claude only) |
| `-stream_thinking` | bool | true | Stream thinking process in real-time |
| `-output_thinking` | bool | false | Include thinking output in final response |
| `-view` | bool | false | View conversation history |
| `-image` | bool | false | Include clipboard image in request |
| `-web` | bool | false | Enable web search in requests |
| `-pdf` | string | "" | Path to PDF file to include in request |
| `-chunk` | string | "" | Path to markdown file for chunking and embedding |

---

## Model Selection

### Available Models

Use the `-model` flag to select which AI model to use:

```bash
# Claude Models (Anthropic)
./owl -model claude              # Default Claude model (latest)
./owl -model opus                # Claude 3 Opus (most capable, slower)
./owl -model sonnet              # Claude 3 Sonnet (balanced)
./owl -model haiku               # Claude 3 Haiku (fast, lightweight)

# OpenAI Models
./owl -model 4o                  # GPT-4 Omni (multimodal)
./owl -model gpt                 # GPT-4 (standard)
./owl -model codex               # Codex (code generation)

# Other Models
./owl -model grok                # Grok (xAI)
./owl -model qwen3               # Qwen 3 (via Ollama, local)
```

### Model-Specific Options

**Claude Models** support extended thinking:
```bash
./owl -model sonnet -thinking -stream_thinking -output_thinking
```

**Parameters:**
- `-thinking` - Enable thinking mode (compute-heavy, more accurate)
- `-stream_thinking` - Show thinking process in real-time
- `-output_thinking` - Include thinking tokens in final response

---

## Usage Examples

### Example 1: Basic Chat
```bash
./owl -prompt "Explain quantum computing"
```

### Example 2: Streaming Response
```bash
./owl -prompt "Write a Python function for sorting" -stream
```

### Example 3: With Extended Thinking (Claude)
```bash
./owl -model opus -prompt "Solve this logic puzzle: ..." -thinking -output_thinking
```

### Example 4: Context-Based Conversation
```bash
# Create a new context called "project_x"
./owl -context_name "project_x" -prompt "What's the project scope?"

# Follow-up in the same context
./owl -context_name "project_x" -prompt "What are the requirements?" -history 5
```

### Example 5: Using Embeddings & RAG
```bash
# Store a document as embeddings
./owl -chunk docs/technical_guide.md -embeddings -store_backend duckdb

# Search and query against stored embeddings
./owl -search "How do I configure the database?" -store_backend duckdb
```

### Example 6: With Attachments
```bash
# Include an image from clipboard
./owl -prompt "Describe this image" -image

# Include a PDF document
./owl -prompt "Summarize this document" -pdf path/to/document.pdf

# Enable web search
./owl -prompt "Latest news about AI" -web
```

### Example 7: System Prompt Configuration
```bash
# Set custom system prompt for a context
./owl -context_name "code_assistant" \
  -system "You are an expert Go programmer. Answer questions about Go development."

# Use the context with the system prompt
./owl -context_name "code_assistant" -prompt "How do goroutines work?"
```

### Example 8: View Conversation History
```bash
# View all conversations in a context
./owl -view -context_name "project_x"

# View with more history items
./owl -view -context_name "project_x" -history 50
```

### Example 9: Launch TUI Mode
```bash
./owl -tui
```
Start the interactive terminal interface for a full-featured chat experience.

### Example 10: Run as API Server
```bash
# Start server on default port 3000
./owl -serve

# Start on custom port with HTTPS
./owl -serve -port 8080 -secure
```

---

## Environment Variables

### Required
- `ANTHROPIC_API_KEY` - For Claude models
- `OPENAI_API_KEY` - For OpenAI models (GPT-4, etc.)

### Optional Configuration
```bash
# Database configuration
OWL_LOCAL_DATABASE=owl              # SQLite database name (default: owl)

# Model-specific
GROK_API_KEY=your_grok_key
OLLAMA_HOST=http://localhost:11434 # For local Ollama models

# Google Cloud (for Vertex AI integration)
GOOGLE_CLOUD_PROJECT=your_project
GOOGLE_APPLICATION_CREDENTIALS=/path/to/credentials.json
```
---

## Available Skills/Tools

### 1. **Git Tool** (`git_tool.go`)
Executes git commands to get repository information and manage version control.

**Capabilities:**
- Show repository status and changed files
- Display current branch
- View recent commits
- Show diffs and changes summary
- Display uncommitted changes

**Parameters:**
- `Action` (string): Git action to perform
  - `status` - Show changed files
  - `branch` - Show current branch
  - `log` - Show recent commits
  - `diff` - Show changes summary
  - `uncommitted` - Show full diff of changes
- `Limit` (optional, integer): Number of commits to show (default: 10)

---

### 2. **HTTP Request Tool** (`http_request_tool.go`)
Makes HTTP requests to external APIs and services.

**Capabilities:**
- Supports GET, POST, PUT, DELETE, PATCH methods
- Custom headers support
- Request body for POST/PUT/PATCH operations
- API integration and webhook support

**Parameters:**
- `URL` (string, required): Full URL for the request
- `Method` (string): HTTP method - GET, POST, PUT, DELETE, PATCH (default: GET)
- `Headers` (string): HTTP headers separated by semicolons (format: `HeaderName: Value; AnotherHeader: Value`)
- `Body` (string): Request body for POST, PUT, or PATCH requests (typically JSON)

---

### 3. **File Operations Tools**

#### 3a. **Read File Tool** (`read_file_tool.go`)
Fetches and displays contents of specified files.

**Capabilities:**
- Read single or multiple files
- Supports various file extensions: .go, .md, .tsx, .ts, .csv, .js, .txt, .mod, .cs, .csproj, .gitignore, .jsx, .json

**Parameters:**
- `FileNames` (string): File names with path; multiple files separated by semicolon (e.g., `README.md;package.json`)

---

#### 3b. **Write File Tool** (`write_file_tool.go`)
Creates new files or overwrites existing files.

**Capabilities:**
- Create new files with content
- Overwrite existing files
- Write to any path relative to current directory

**Parameters:**
- `FileName` (string, required): Path and name of file to write (relative to current directory)
- `Content` (string, required): Content to write to the file

---

#### 3c. **Update File Tool** (`update_file_tool.go`)
Updates specific parts of existing files without replacing entire content.

**Capabilities:**
- Update an existing file using unified diff format
- Show an integrated approval workspace in TUI mode without starting a nested terminal UI
- Preserve the current conversation beside/above the proposed diff during review
- Fall back to standalone Bubble Tea, tmux, or terminal approval outside the TUI

**Parameters:**
- `FileName` (string, required): Path of file to update
- `Diff` (string, required): Unified diff format patch
- `Summary` (string, required): Concise user-facing rationale shown during approval

---

#### 3d. **List Files Tool** (`list_files_tool.go`)
Lists all files in and under a specified directory.

**Capabilities:**
- Display project structure
- Show directory hierarchy
- Filter by file extensions

**Parameters:**
- `Filter` (string, optional): File extensions to filter by (comma-separated)

---

### 4. **Todo Tool** (`todo_tool.go`)
Creates and manages todo items for action tracking.

**Capabilities:**
- Extract action items from emails/messages
- Create actionable todo items
- Set due dates
- Track follow-ups

**Parameters:**
- `Title` (string, required): Concise, actionable title
- `Description` (string, optional): Detailed description and context
- `DueDate` (string, optional): Days from today (e.g., `3` for 3 days from now, `7` for one week)

---

### 5. **Image Generation Tool** (`generate_image_tool.go`)
Generates images from text prompts using AI.

**Capabilities:**
- Create images from descriptions
- Support for various styles
- Returns image as base64
- Saves generated images as PNG

**Parameters:**
- `Prompt` (string, required): Description of the image to generate

---

### 6. **Diff Viewer Tools**

#### 8a. **Diff Viewer** (`diff_viewer.go`)
Displays code differences and changes.

**Capabilities:**
- View file diffs
- Highlight changes
- Compare versions

---

#### 8b. **Diff Viewer TMUX** (`diff_viewer_tmux.go`)
Enhanced diff viewer using TMUX for terminal multiplexing.

**Capabilities:**
- Display diffs in TMUX panes
- Better visualization in terminal environments

---

### 7. **Tool Response Handler** (`tool_response_handler.go`)
Manages responses from tool executions.

**Capabilities:**
- Process tool outputs
- Format responses
- Error handling

---

### 8. **Tool Runner** (`tool_runner.go`)
Executes and orchestrates tool operations.

**Capabilities:**
- Execute registered tools
- Manage tool lifecycle
- Handle tool dependencies

---

### 9. **Stock Price Lookup Tool** (`stock_price_lookup_tool.go`)
Fetches current stock/index quotes from Yahoo Finance without API keys.

**Tool Name:** `stock_price_lookup`

**Modes:**
- `single` - one symbol via `Symbol`
- `custom_list` - comma-separated symbols via `Symbols`
- `general_market` - grouped defaults for global/swedish/american/asian
- `my_standards` - personal default watchlist

**General Market Defaults:**
- `global`: `VT`, `ACWI`, `URTH`
- `swedish`: `^OMXS30`, `EQT.ST`, `ATCO-A.ST`, `VOLV-B.ST`
- `american`: `^GSPC`, `^IXIC`, `^DJI`, `^RUT`
- `asian`: `^N225`, `^HSI`, `000001.SS`, `^KS11`

**My Standards Defaults:**
- `AAPL`, `TSLA`, `MSFT`, `MU`, `IBM`, `GOOGL`, `META`, `GME`

**Parameters:**
- `Mode` (`single`, `custom_list`, `general_market`, `my_standards`)
- `Symbol` (required in `single` mode)
- `Symbols` (required in `custom_list` mode)

---

## Supported AI Models

The application integrates with multiple AI providers:

- **Claude** (Anthropic) - via Vertex AI and direct API
- **OpenAI Models**:
  - GPT-4
  - GPT-4 Vision
  - OpenAI Base Models
- **Grok** (xAI)
- **Ollama** (Local models)

---

## Database Support

- **SQLite** - Default local database
- **PostgreSQL** - For multi-user deployments
- **DuckDB** - For analytics and embeddings
- **SQLite-Vec** - Vector embeddings support

---

## Key Features

### Text Processing
- PDF document processing
- Code extraction from documents
- Text chunking and segmentation
- Clipboard integration

### User Management
- Multi-user support with SQLite
- User history tracking
- Embeddings database per user

### Chat & History
- Full chat history repository
- Conversation persistence
- History search and retrieval

### TUI Interface
- Terminal-based chat view
- List view for navigation
- Chat history view
- Customizable styles

---

## Usage Patterns

### File Management Workflow
```
1. List files to understand structure (list_files_tool)
2. Read specific files for context (read_file_tool)
3. Make updates (update_file_tool or write_file_tool)
4. Verify changes (git_tool with diff action)
```

### Development Workflow
```
1. Check repository status (git_tool status)
2. View recent commits (git_tool log)
3. Read code files (read_file_tool)
4. Update implementation (update_file_tool)
5. Run focused validation or checks for the updated area
```

### Integration Workflow
```
1. Make HTTP requests to external APIs (http_request_tool)
2. Process responses
3. Create todos for follow-ups (todo_tool)
4. Update documentation (write_file_tool)
```

---

## Tool Constraints & Best Practices

### File Operations
- Paths must be relative to current directory
- No parent directory references (..) allowed
- Parent directories must exist before creating files in subdirectories

### HTTP Requests
- Headers formatted as semicolon-separated key-value pairs
- Request body typically JSON for POST/PUT/PATCH
- Include proper Content-Type headers

### Image Generation
- Limit to 1-2 images per request (time-intensive)
- Provide detailed, descriptive prompts
- Results saved as PNG files

### Reading Files
- Prefers specific extensions: .go, .md, .tsx, .ts, .csv, .js, .txt, .mod, .cs, .csproj, .gitignore, .jsx, .json
- Avoid overuse to manage token consumption

---

## Configuration

The application supports:
- Environment variables via `.env` files
- Google Cloud Secret Manager integration
- Multiple database backends
- Various AI model configurations

---

## Version Information

- **Language**: Go 1.24.0
- **Project Name**: owl
- **Architecture**: Modular tool-based system with plugin capability

---

## Additional Resources

See the following documentation for more details:
- `CHUNKING.md` - Text chunking implementation
- `README.md` - Project overview
- Individual tool files in `/tools` directory
- Model documentation in `/models` directory
