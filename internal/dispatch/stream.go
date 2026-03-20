package dispatch

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jorge-barreto/orc/internal/ux"
)

// PermissionDenial represents a tool that was denied by the permission system.
type PermissionDenial struct {
	Tool  string
	Input string
}

// String returns a human-readable summary of the denial.
func (d PermissionDenial) String() string {
	if d.Input != "" {
		return fmt.Sprintf("%s(%s)", d.Tool, d.Input)
	}
	return d.Tool
}

// UserQuestion represents an AskUserQuestion tool call from the agent.
type UserQuestion struct {
	Question string
	Options  []string
}

// StreamResult holds the parsed output from a stream-json claude invocation.
type StreamResult struct {
	Text                     string
	PermissionDenials        []PermissionDenial
	UserQuestions            []UserQuestion
	CostUSD                  float64
	SessionID                string
	InputTokens              int
	OutputTokens             int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
}

// streamState tracks tool use accumulation across stream events.
type streamState struct {
	toolName      string
	inputBuf      strings.Builder
	hadText       bool
	userQuestions []UserQuestion
}

// warnWriter wraps an io.Writer and logs the first write error to stderr.
// After the first error, subsequent writes are silently dropped.
type warnWriter struct {
	w      io.Writer
	failed bool
}

func (ww *warnWriter) Write(p []byte) (int, error) {
	if ww.failed {
		return len(p), nil
	}
	n, err := ww.w.Write(p)
	if err != nil {
		ww.failed = true
		fmt.Fprintf(os.Stderr, "warning: raw log write failed: %v\n", err)
		return len(p), nil
	}
	return n, nil
}

// ProcessStream reads stream-json lines from stdout, routes text to display+log,
// tracks tool use for inline display, and extracts the final result.
func ProcessStream(ctx context.Context, stdout io.Reader, display io.Writer, logFile io.Writer, rawLog io.Writer) (*StreamResult, error) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

	var result StreamResult
	var textBuf strings.Builder
	var ss streamState

	var safeRawLog io.Writer
	if rawLog != nil {
		safeRawLog = &warnWriter{w: rawLog}
	}

	for scanner.Scan() {
		if ctx.Err() != nil {
			return &result, ctx.Err()
		}

		line := scanner.Bytes()
		if safeRawLog != nil {
			safeRawLog.Write(line)
			safeRawLog.Write([]byte{'\n'})
		}
		if len(line) == 0 {
			continue
		}

		var event streamEvent
		if err := json.Unmarshal(line, &event); err != nil {
			// Skip malformed lines
			continue
		}

		switch event.Type {
		case "stream_event":
			handleStreamEvent(&event, &textBuf, &ss, display, logFile)

		case "result":
			handleResultEvent(&event, &result)
		}
	}

	if err := scanner.Err(); err != nil {
		return &result, fmt.Errorf("reading stream: %w", err)
	}

	result.Text = textBuf.String()
	result.UserQuestions = ss.userQuestions
	return &result, nil
}

// streamEvent is the top-level JSON structure from stream-json output.
type streamEvent struct {
	Type      string          `json:"type"`
	Event     json.RawMessage `json:"event"`
	Message   json.RawMessage `json:"message"`
	SessionID string          `json:"session_id"`

	// Fields for "result" type
	Result            json.RawMessage   `json:"result"`
	TotalCostUSD      float64           `json:"total_cost_usd"`
	Usage             *resultUsage      `json:"usage"`
	PermissionDenials []permDenialEntry `json:"permission_denials"`

	// Fields for "user"/"assistant" message types
	Content []contentBlock `json:"content"`
	IsError bool           `json:"is_error"`
}

// contentBlock represents a content item in assistant/user messages.
type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
}

// nestedEvent is the inner event from stream_event messages.
type nestedEvent struct {
	Type         string        `json:"type"`
	ContentBlock *contentBlock `json:"content_block"`
	Delta        *deltaBlock   `json:"delta"`
}

// deltaBlock holds the delta in content_block_delta events.
type deltaBlock struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	PartialJSON string `json:"partial_json"`
}

// resultUsage holds token counts from the result event's usage object.
type resultUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

type permDenialEntry struct {
	ToolName string `json:"tool_name"`
	Input    string `json:"input"`
}

func handleStreamEvent(event *streamEvent, textBuf *strings.Builder, ss *streamState, display io.Writer, logFile io.Writer) {
	if event.Event == nil {
		return
	}

	var nested nestedEvent
	if err := json.Unmarshal(event.Event, &nested); err != nil {
		return
	}

	switch nested.Type {
	case "content_block_start":
		if nested.ContentBlock != nil && nested.ContentBlock.Type == "tool_use" {
			ss.toolName = nested.ContentBlock.Name
			ss.inputBuf.Reset()
		}

	case "content_block_delta":
		if nested.Delta == nil {
			return
		}
		switch nested.Delta.Type {
		case "text_delta":
			text := nested.Delta.Text
			textBuf.WriteString(text)
			if display != nil {
				fmt.Fprint(display, text)
			}
			if logFile != nil {
				fmt.Fprint(logFile, text)
			}
			ss.hadText = true
		case "input_json_delta":
			ss.inputBuf.WriteString(nested.Delta.PartialJSON)
		}

	case "content_block_stop":
		if ss.toolName != "" {
			if ss.toolName == "AskUserQuestion" {
				var input struct {
					Question string   `json:"question"`
					Options  []string `json:"options"`
				}
				if err := json.Unmarshal([]byte(ss.inputBuf.String()), &input); err == nil && input.Question != "" {
					ss.userQuestions = append(ss.userQuestions, UserQuestion{
						Question: input.Question,
						Options:  input.Options,
					})
				}
			}
			summary := toolUseSummary(ss.toolName, ss.inputBuf.String())
			if ss.hadText && display != nil {
				fmt.Fprint(display, "\n")
			}
			if ss.hadText && logFile != nil {
				fmt.Fprint(logFile, "\n")
			}
			ux.ToolUse(ss.toolName, summary)
			if logFile != nil {
				fmt.Fprintf(logFile, "⚡ %s %s\n", ss.toolName, summary)
			}
			ss.toolName = ""
			ss.inputBuf.Reset()
			ss.hadText = false
		}
	}
}

// toolUseSummary extracts the most informative field from accumulated tool input JSON.
func toolUseSummary(toolName, rawJSON string) string {
	if rawJSON == "" {
		return ""
	}

	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(rawJSON), &obj); err != nil {
		return rawJSON
	}

	// Pick the most informative key based on tool name.
	var key string
	switch toolName {
	case "Bash":
		key = "command"
	case "Read", "Write", "Edit":
		key = "file_path"
	case "Grep", "Glob":
		key = "pattern"
	case "Task", "TaskCreate":
		key = "description"
	case "AskUserQuestion":
		key = "question"
	default:
		// Fall back to first string value.
		for _, v := range obj {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return rawJSON
	}

	if v, ok := obj[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return rawJSON
}

func handleResultEvent(event *streamEvent, result *StreamResult) {
	if event.TotalCostUSD > 0 {
		result.CostUSD = event.TotalCostUSD
	}
	if event.Usage != nil {
		if event.Usage.InputTokens > 0 {
			result.InputTokens = event.Usage.InputTokens
		}
		if event.Usage.OutputTokens > 0 {
			result.OutputTokens = event.Usage.OutputTokens
		}
		if event.Usage.CacheCreationInputTokens > 0 {
			result.CacheCreationInputTokens = event.Usage.CacheCreationInputTokens
		}
		if event.Usage.CacheReadInputTokens > 0 {
			result.CacheReadInputTokens = event.Usage.CacheReadInputTokens
		}
	}
	for _, d := range event.PermissionDenials {
		result.PermissionDenials = append(result.PermissionDenials, PermissionDenial{
			Tool:  d.ToolName,
			Input: d.Input,
		})
	}
	if event.SessionID != "" {
		result.SessionID = event.SessionID
	}
}
