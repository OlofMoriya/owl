package data

import (
	"database/sql"
	"fmt"
	"log"
	"owl/logger"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func (user User) getUserDb() *sql.DB {

	homeDir, err := getHomeDir()
	if err != nil {
		panic(fmt.Sprintf("did not find home dir for db creation. %s", err))
	}

	path := fmt.Sprintf("%s/.owl/%s.db", homeDir, *user.Name)

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		panic(err)
	}

	// Check if the database is new by querying the sqlite_master table
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master").Scan(&count)
	if err != nil {
		panic(err)
	}

	// If the database is new (no tables exist), set it up
	if count == 0 {
		user.setupDb(db)
	} else {
		user.ensureArchivedColumnsExist(db)
		user.ensureTokenColumnsExist(db)
		user.ensureContextPreferenceColumnsExist(db)
		user.ensureToolTablesExist(db)
	}

	return db
}

func (user User) setupDb(db *sql.DB) {
	createHistoryTables(db)
	createToolTables(db)
	createContextTable(db)
	user.ensureArchivedColumnsExist(db)
	user.ensureTokenColumnsExist(db)
	user.ensureContextPreferenceColumnsExist(db)
	user.ensureToolTablesExist(db)
}

func createContextTable(db *sql.DB) {
	createTableQuery := `
		 CREATE TABLE IF NOT EXISTS context (
	             id INTEGER PRIMARY KEY AUTOINCREMENT,
	             name TEXT,
			 system_prompt TEXT,
			 preferred_model TEXT,
			 preferred_agent TEXT,
			 preferred_skills TEXT,
			 archived INTEGER DEFAULT 0
	         )
     `

	_, err := db.Exec(createTableQuery)
	if err != nil {
		panic(err)
	}
}

func createHistoryTables(db *sql.DB) {
	createTableQuery := `
         CREATE TABLE IF NOT EXISTS history (
             id INTEGER PRIMARY KEY AUTOINCREMENT,
             context_id INTEGER,
             prompt TEXT,
             response TEXT,
			 response_content TEXT,
             abreviation TEXT,
             token_count INTEGER,
		 prompt_tokens INTEGER DEFAULT 0,
		 completion_tokens INTEGER DEFAULT 0,
		 cache_read_tokens INTEGER DEFAULT 0,
		 cache_write_tokens INTEGER DEFAULT 0,
		 created INT,
		 tool_results TEXT,
		 model TEXT,
		 archived INTEGER DEFAULT 0
         )
     `
	_, err := db.Exec(createTableQuery)
	if err != nil {
		panic(err)
	}
}

func createToolTables(db *sql.DB) {
	createToolUseTableQuery := `
		CREATE TABLE IF NOT EXISTS tool_use (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			history_id INTEGER,
			tool_use_external_id TEXT,
			name TEXT,
			input TEXT,
			caller_type TEXT DEFAULT 'assistant',
			created INT,
			FOREIGN KEY(history_id) REFERENCES history(id) ON DELETE CASCADE
		)
	`

	_, err := db.Exec(createToolUseTableQuery)
	if err != nil {
		panic(err)
	}

	createToolResultTableQuery := `
		CREATE TABLE IF NOT EXISTS tool_result (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tool_use_id INTEGER UNIQUE,
			content TEXT,
			success INTEGER DEFAULT 1,
			created INT,
			FOREIGN KEY(tool_use_id) REFERENCES tool_use(id) ON DELETE CASCADE
		)
	`

	_, err = db.Exec(createToolResultTableQuery)
	if err != nil {
		panic(err)
	}
}

func (user User) ensureArchivedColumnsExist(db *sql.DB) {
	// Add archived column to context if it doesn't exist
	_, _ = db.Exec("ALTER TABLE context ADD COLUMN archived INTEGER DEFAULT 0")

	// Add archived column to history if it doesn't exist
	_, _ = db.Exec("ALTER TABLE history ADD COLUMN archived INTEGER DEFAULT 0")
}

func (user User) ensureTokenColumnsExist(db *sql.DB) {
	_, _ = db.Exec("ALTER TABLE history ADD COLUMN prompt_tokens INTEGER DEFAULT 0")
	_, _ = db.Exec("ALTER TABLE history ADD COLUMN completion_tokens INTEGER DEFAULT 0")
	_, _ = db.Exec("ALTER TABLE history ADD COLUMN cache_read_tokens INTEGER DEFAULT 0")
	_, _ = db.Exec("ALTER TABLE history ADD COLUMN cache_write_tokens INTEGER DEFAULT 0")
}

func (user User) ensureContextPreferenceColumnsExist(db *sql.DB) {
	_, _ = db.Exec("ALTER TABLE context ADD COLUMN preferred_agent TEXT")
	_, _ = db.Exec("ALTER TABLE context ADD COLUMN preferred_skills TEXT")
}

func (user User) ensureToolTablesExist(db *sql.DB) {
	createToolTables(db)
}

func (user User) ArchiveContext(contextId int64, archived bool) error {
	db := user.getUserDb()
	defer db.Close()
	val := 0
	if archived {
		val = 1
	}
	_, err := db.Exec("UPDATE context SET archived = ? WHERE id = ?", val, contextId)
	return err
}

func (user User) ArchiveHistory(historyId int64, archived bool) error {
	db := user.getUserDb()
	defer db.Close()
	val := 0
	if archived {
		val = 1
	}
	_, err := db.Exec("UPDATE history SET archived = ? WHERE id = ?", val, historyId)
	return err
}

func (user User) InsertContext(context Context) (int64, error) {
	db := user.getUserDb()

	logger.Debug.Printf("inserting context %v, %v, %v", context.Name, user.Name, user.Id)

	insertQuery := "INSERT INTO context (name, system_prompt, preferred_model, preferred_agent, preferred_skills) VALUES (?, ?, ?, ?, ?)"
	result, err := db.Exec(insertQuery, context.Name, context.SystemPrompt, context.PreferredModel, context.PreferredAgent, context.PreferredSkills)
	logger.Debug.Println("result of context insert", result)

	defer db.Close()
	if err != nil {
		log.Println("insert of context failed", err)
		return 0, err
	}

	contextId, err := result.LastInsertId()
	if err != nil {
		log.Println("insert of context failed", err)
		return 0, err
	}

	return contextId, nil
}

func (user User) GetContextById(contextId int64) (Context, error) {
	db := user.getUserDb()
	defer db.Close()

	// When getting by ID, we unarchive it as it is being "used"
	_, _ = db.Exec("UPDATE context SET archived = 0 WHERE id = ?", contextId)

	selectQuery := "SELECT id, name, system_prompt, COALESCE(preferred_model, 'sonnet'), COALESCE(preferred_agent, ''), COALESCE(preferred_skills, ''), archived FROM context WHERE id = ?"
	row := db.QueryRow(selectQuery, contextId)

	var context Context
	var archived int
	err := row.Scan(&context.Id, &context.Name, &context.SystemPrompt, &context.PreferredModel, &context.PreferredAgent, &context.PreferredSkills, &archived)
	context.Archived = archived == 1
	if err != nil {
		if err == sql.ErrNoRows {
			// return context, fmt.Errorf("context with ID %d not found", contextId)
		}
		return context, err
	}

	return context, nil
}

func (user User) InsertHistory(history History) (int64, error) {
	db := user.getUserDb()
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}

	insertQuery := "INSERT INTO history (context_id, prompt, response, abreviation, token_count, prompt_tokens, completion_tokens, cache_read_tokens, cache_write_tokens, response_content, created, tool_results, model) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
	result, err := tx.Exec(insertQuery, history.ContextId, history.Prompt, history.Response, history.Abbreviation, history.TokenCount, history.PromptTokens, history.CompletionTokens, history.CacheReadTokens, history.CacheWriteTokens, history.ResponseContent, time.Now(), history.ToolResults, history.Model)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}

	historyId, err := result.LastInsertId()
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}

	for _, toolUse := range history.ToolUse {
		callerType := strings.TrimSpace(toolUse.CallerType)
		if callerType == "" {
			callerType = "assistant"
		}

		toolUseInsertQuery := "INSERT INTO tool_use (history_id, tool_use_external_id, name, input, caller_type, created) VALUES (?, ?, ?, ?, ?, ?)"
		toolUseResult, err := tx.Exec(toolUseInsertQuery, historyId, toolUse.Id, toolUse.Name, toolUse.Input, callerType, time.Now())
		if err != nil {
			_ = tx.Rollback()
			return 0, err
		}

		toolUseId, err := toolUseResult.LastInsertId()
		if err != nil {
			_ = tx.Rollback()
			return 0, err
		}

		success := 0
		if toolUse.Result.Success {
			success = 1
		}

		toolResultInsertQuery := "INSERT INTO tool_result (tool_use_id, content, success, created) VALUES (?, ?, ?, ?)"
		_, err = tx.Exec(toolResultInsertQuery, toolUseId, toolUse.Result.Content, success, time.Now())
		if err != nil {
			_ = tx.Rollback()
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return 0, err
	}

	return historyId, nil
}

func (user User) GetHistoryByContextId(contextId int64, maxCount int) ([]History, error) {
	db := user.getUserDb()
	defer db.Close()

	//logger.Debug.Printf("Fetching history for contextId: %v, maxCount: %v", contextId, maxCount)
	selectQuery := "SELECT id, context_id, prompt, response, response_content, abreviation, token_count, prompt_tokens, completion_tokens, cache_read_tokens, cache_write_tokens, created, tool_results, COALESCE(model, 'sonnet'), archived FROM history WHERE context_id = ? ORDER BY ID DESC LIMIT ?"
	rows, err := db.Query(selectQuery, contextId, maxCount)
	if err != nil {
		logger.Debug.Printf("Error in sql %s", err)
		return nil, err
	}

	var histories []History
	for rows.Next() {
		var history History
		var archived int
		err := rows.Scan(&history.Id, &history.ContextId, &history.Prompt, &history.Response, &history.ResponseContent, &history.Abbreviation, &history.TokenCount, &history.PromptTokens, &history.CompletionTokens, &history.CacheReadTokens, &history.CacheWriteTokens, &history.Created, &history.ToolResults, &history.Model, &archived)
		if err != nil {
			return nil, err
		}
		history.Archived = archived == 1
		history.ToolUse = []ToolUse{}
		histories = append(histories, history)
	}

	historyIDs := make([]int64, 0, len(histories))
	historyIdx := make(map[int64]int, len(histories))
	for i := range histories {
		historyIDs = append(historyIDs, histories[i].Id)
		historyIdx[histories[i].Id] = i
	}

	if len(historyIDs) > 0 {
		placeholders := make([]string, len(historyIDs))
		queryArgs := make([]interface{}, len(historyIDs))
		for i, id := range historyIDs {
			placeholders[i] = "?"
			queryArgs[i] = id
		}

		toolQuery := fmt.Sprintf("SELECT tu.history_id, tu.tool_use_external_id, tu.name, tu.input, COALESCE(tu.caller_type, 'assistant'), COALESCE(tr.content, ''), COALESCE(tr.success, 1) FROM tool_use tu LEFT JOIN tool_result tr ON tr.tool_use_id = tu.id WHERE tu.history_id IN (%s) ORDER BY tu.history_id ASC, tu.id ASC", strings.Join(placeholders, ","))
		toolRows, err := db.Query(toolQuery, queryArgs...)
		if err != nil {
			return nil, err
		}
		defer toolRows.Close()

		for toolRows.Next() {
			var historyId int64
			var toolUse ToolUse
			var success int

			err := toolRows.Scan(&historyId, &toolUse.Id, &toolUse.Name, &toolUse.Input, &toolUse.CallerType, &toolUse.Result.Content, &success)
			if err != nil {
				return nil, err
			}

			toolUse.Result.ToolUseId = toolUse.Id
			toolUse.Result.Success = success == 1

			if idx, ok := historyIdx[historyId]; ok {
				histories[idx].ToolUse = append(histories[idx].ToolUse, toolUse)
			}
		}

		if err := toolRows.Err(); err != nil {
			return nil, err
		}
	}

	for i, j := 0, len(histories)-1; i < j; i, j = i+1, j-1 {
		histories[i], histories[j] = histories[j], histories[i]
	}
	defer rows.Close()

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return histories, nil
}

func (user User) GetContextByName(name string) (*Context, error) {
	db := user.getUserDb()
	defer db.Close()

	// When getting by name, we unarchive it as it is being "used"
	_, _ = db.Exec("UPDATE context SET archived = 0 WHERE name = ?", name)

	selectQuery := "SELECT id, name, system_prompt, COALESCE(preferred_model, 'sonnet'), COALESCE(preferred_agent, ''), COALESCE(preferred_skills, ''), archived FROM context WHERE name = ?"
	row := db.QueryRow(selectQuery, name)

	var context Context
	var archived int
	err := row.Scan(&context.Id, &context.Name, &context.SystemPrompt, &context.PreferredModel, &context.PreferredAgent, &context.PreferredSkills, &archived)
	context.Archived = archived == 1

	if err != nil {
		return nil, err
	}

	return &context, err
}

func (user User) GetAllContexts() ([]Context, error) {
	db := user.getUserDb()
	defer db.Close()

	rows, err := db.Query("SELECT id, name, system_prompt, COALESCE(preferred_model, 'sonnet'), COALESCE(preferred_agent, ''), COALESCE(preferred_skills, ''), archived FROM context")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var contexts []Context
	for rows.Next() {
		var context Context
		var archived int
		err := rows.Scan(&context.Id, &context.Name, &context.SystemPrompt, &context.PreferredModel, &context.PreferredAgent, &context.PreferredSkills, &archived)
		if err != nil {
			return nil, err
		}
		context.Archived = archived == 1
		contexts = append(contexts, context)
	}

	err = rows.Err()
	if err != nil {
		return nil, err
	}

	return contexts, nil
}

func (user User) DeleteContext(contextId int64) (int64, error) {
	db := user.getUserDb()
	defer db.Close()

	res, err := db.Exec("DELETE FROM context WHERE id = ?", contextId)
	//TODO: should also delete all history connected to the context

	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (user User) DeleteHistory(historyId int64) (int64, error) {
	db := user.getUserDb()
	defer db.Close()

	res, err := db.Exec("DELETE FROM history WHERE id = ?", historyId)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (user User) UpdateSystemPrompt(contextId int64, systemPrompt string) error {
	db := user.getUserDb()
	defer db.Close()

	fmt.Printf("setting system %s %d :::", systemPrompt, contextId)

	_, err := db.Exec("UPDATE context SET system_prompt = ? WHERE id = ?",
		systemPrompt, contextId)
	return err
}

func (user User) UpdatePreferredModel(contextId int64, model string) error {
	db := user.getUserDb()
	defer db.Close()

	_, err := db.Exec("UPDATE context SET preferred_model = ? WHERE id = ?",
		model, contextId)
	return err
}

func (user User) UpdatePreferredAgent(contextId int64, agent string) error {
	db := user.getUserDb()
	defer db.Close()

	_, err := db.Exec("UPDATE context SET preferred_agent = ? WHERE id = ?", agent, contextId)
	return err
}

func (user User) UpdatePreferredSkills(contextId int64, skills string) error {
	db := user.getUserDb()
	defer db.Close()

	_, err := db.Exec("UPDATE context SET preferred_skills = ? WHERE id = ?", skills, contextId)
	return err
}
