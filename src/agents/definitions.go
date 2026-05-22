package agents

import (
	"fmt"
	"strings"

	"owl/tools"
)

type Definition struct {
	Name         string
	DisplayName  string
	AccentColor  string
	Description  string
	SystemPrompt string
	ToolGroups   []tools.ToolGroup
}

var defaultOrder = []string{"planner", "developer", "manager", "secretary"}

var defaults = map[string]Definition{
	"planner": {
		Name:        "planner",
		DisplayName: "Planner",
		AccentColor: "yellow",
		Description: "Read-only planning and implementation strategy",
		SystemPrompt: "You are Owl Planner. Focus on read-only analysis, implementation planning, and risk assessment. " +
			"Do not edit files in planning mode. Use available read-only tools when needed to inspect code, gather facts, and validate assumptions before answering. " +
			"Do not claim you inspected files, ran checks, or gathered evidence unless you actually used tools to do so. " +
			"Provide concrete step-by-step plans and validation checklists.",
		ToolGroups: []tools.ToolGroup{tools.ToolGroupPlanner},
	},
	"developer": {
		Name:        "developer",
		DisplayName: "Developer",
		AccentColor: "blue",
		Description: "Implementation-focused coding agent",
		SystemPrompt: "You are Owl Developer. Implement approved plans with minimal, safe diffs. " +
			"Use available tools proactively whenever they are needed to complete the task (for example: read files before editing, edit files to apply changes, run checks/tests to validate). " +
			"Do not say you made changes, ran commands, or verified results unless you actually performed those actions with tools. " +
			"If a task requires file changes or verification, perform them with tools instead of only describing intent. " +
			"Follow repository conventions, run relevant validation, and clearly report what changed and why.",
		ToolGroups: []tools.ToolGroup{tools.ToolGroupDeveloper},
	},
	"manager": {
		Name:        "manager",
		DisplayName: "Manager",
		AccentColor: "green",
		Description: "Work/life management assistant",
		SystemPrompt: "You are Owl Manager Assistant. Help organize priorities, commitments, and next actions. " +
			"Be concise, practical, and deadline-aware.",
		ToolGroups: []tools.ToolGroup{tools.ToolGroupManager},
	},
	"secretary": {
		Name:        "secretary",
		DisplayName: "Secretary",
		AccentColor: "cyan",
		Description: "Communication and follow-up assistant",
		SystemPrompt: "You are Owl Secretary Assistant. Draft clear communication, track follow-ups, and support scheduling tasks. " +
			"Confirm key details before proposing final messages.",
		ToolGroups: []tools.ToolGroup{tools.ToolGroupSecretary},
	},
}

func Get(name string) (Definition, bool) {
	def, ok := defaults[strings.ToLower(strings.TrimSpace(name))]
	return def, ok
}

func Resolve(name string) (Definition, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return Definition{}, nil
	}

	def, ok := Get(trimmed)
	if !ok {
		return Definition{}, fmt.Errorf("unknown agent %q (valid: planner, developer, manager, secretary)", name)
	}

	return def, nil
}

func List() []Definition {
	items := make([]Definition, 0, len(defaultOrder))
	for _, name := range defaultOrder {
		if def, ok := defaults[name]; ok {
			items = append(items, def)
		}
	}
	return items
}
