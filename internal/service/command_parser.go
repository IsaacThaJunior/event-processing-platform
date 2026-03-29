// internal/service/command_parser.go
package service

import (
	"encoding/json"
	"strings"
)

// CommandParser handles parsing natural language commands
type CommandParser struct{}

// ParseResult contains the parsed command and parameters
type ParseResult struct {
	Command string `json:"command"`
	Payload string `json:"payload"`
}

func NewCommandParser() *CommandParser {
	return &CommandParser{}
}

// ParseCommand extracts the command and parameters from message text
func (p *CommandParser) ParseCommand(text string) ParseResult {
	text = strings.ToLower(strings.TrimSpace(text))

	// Command patterns
	commands := map[string]func(string) string{
		"resize image": func(rest string) string {
			// Parse: "resize image from [url] to [width]x[height]"
			params := map[string]any{
				"image_url": extractURL(rest),
				"width":     extractWidth(rest),
				"height":    extractHeight(rest),
			}
			jsonBytes, _ := json.Marshal(params)
			return string(jsonBytes)
		},
		"scrape": func(rest string) string {
			// Parse: "scrape [url]"
			params := map[string]any{
				"url": extractURL(rest),
			}
			jsonBytes, _ := json.Marshal(params)
			return string(jsonBytes)
		},
		"generate report": func(rest string) string {
			// Parse: "generate report for [date]"
			params := map[string]any{
				"date": strings.TrimSpace(rest),
			}
			jsonBytes, _ := json.Marshal(params)
			return string(jsonBytes)
		},
	}

	// Check each command pattern
	for cmdPattern, parseFunc := range commands {
		if strings.HasPrefix(text, cmdPattern) {
			rest := strings.TrimPrefix(text, cmdPattern)
			return ParseResult{
				Command: strings.ReplaceAll(cmdPattern, " ", "_"),
				Payload: parseFunc(rest),
			}
		}
	}

	// Default: treat the whole message as a command
	return ParseResult{
		Command: "unknown",
		Payload: `{"text": "` + text + `"}`,
	}
}

// Helper extraction functions
func extractURL(text string) string {
	// Simple URL extraction - can be enhanced
	words := strings.FieldsSeq(text)
	for word := range words {
		if strings.HasPrefix(word, "http://") || strings.HasPrefix(word, "https://") {
			return word
		}
	}
	return ""
}

func extractWidth(text string) int {
	// Find pattern like "800x600" and extract width
	return 800 // Simplified
}

func extractHeight(text string) int {
	return 600 // Simplified
}
