package tools

import (
	"encoding/json"
	"fmt"
	"owl/data"
	"sort"
	"strings"
)

type ToolUsageFormatter interface {
	FormatToolUse(toolUse data.ToolUse) []string
}

func FormatToolUseForDisplay(toolUse data.ToolUse) []string {
	return sanitizeDisplayLines(FormatToolUseForDisplayRaw(toolUse))
}

func FormatToolUseForDisplayRaw(toolUse data.ToolUse) []string {
	if tool, err := GetTool(toolUse.Name); err == nil {
		if formatter, ok := tool.(ToolUsageFormatter); ok {
			lines := formatter.FormatToolUse(toolUse)
			if len(lines) > 0 {
				return lines
			}
		}
	}

	return defaultToolUseDisplayLinesRaw(toolUse)
}

func defaultToolUseDisplayLines(toolUse data.ToolUse) []string {
	return sanitizeDisplayLines(defaultToolUseDisplayLinesRaw(toolUse))
}

func defaultToolUseDisplayLinesRaw(toolUse data.ToolUse) []string {
	status := "✓"
	if !toolUse.Result.Success {
		status = "✗"
	}

	source := ""
	if toolUse.CallerType == "assistant_server" {
		source = " (server)"
	}

	name := strings.TrimSpace(toolUse.Name)
	if name == "" {
		name = "unknown_tool"
	}

	lines := []string{fmt.Sprintf("%s %s%s", name, status, source)}

	inputPreview := previewJSONMap(toolUse.Input)
	if inputPreview != "" {
		lines = append(lines, fmt.Sprintf("input: %s", inputPreview))
	}

	resultPreview := singleLine(toolUse.Result.Content, 100)
	if resultPreview != "" {
		lines = append(lines, fmt.Sprintf("result: %s", resultPreview))
	}
	return lines
}

func ParseToolUseInput(toolUse data.ToolUse) map[string]string {
	parsed := map[string]string{}
	if strings.TrimSpace(toolUse.Input) == "" {
		return parsed
	}

	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(toolUse.Input), &raw); err != nil {
		return parsed
	}

	for key, value := range raw {
		switch v := value.(type) {
		case string:
			parsed[key] = v
		default:
			bytes, err := json.Marshal(v)
			if err != nil {
				continue
			}
			parsed[key] = string(bytes)
		}
	}

	return parsed
}

func singleLine(input string, max int) string {
	s := strings.TrimSpace(input)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.Join(strings.Fields(s), " ")
	if max > 0 && len(s) > max {
		return s[:max-1] + "…"
	}
	return s
}

func previewJSONMap(raw string) string {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil || len(m) == 0 {
		return ""
	}

	parts := make([]string, 0, len(m))
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := m[key]
		switch v := value.(type) {
		case string:
			parts = append(parts, fmt.Sprintf("%s=%s", key, singleLine(v, 30)))
		default:
			bytes, err := json.Marshal(v)
			if err != nil {
				continue
			}
			parts = append(parts, fmt.Sprintf("%s=%s", key, singleLine(string(bytes), 30)))
		}
		if len(parts) >= 3 {
			break
		}
	}

	return singleLine(strings.Join(parts, ", "), 100)
}

func sanitizeDisplayLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		out = append(out, singleLine(trimmed, 160))
	}
	return out
}
