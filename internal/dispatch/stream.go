package dispatch

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// StreamResult holds the parsed output from a stream-json claude invocation.
type StreamResult struct {
	Text              string
	PermissionDenials []PermissionDenial
	CostUSD           float64
	SessionID         string
}

// processStream reads stream-json lines from stdout, routes text to display+log,
// tracks tool use for inline display, and extracts the final result.
func processStream(ctx context.Context, stdout io.Reader, display io.Writer, logFile io.Writer) (*StreamResult, error) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

	var result StreamResult
	var textBuf strings.Builder

	for scanner.Scan() {
		if ctx.Err() != nil {
			return &result, ctx.Err()
		}

		line := scanner.Bytes()
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
			handleStreamEvent(&event, &textBuf, display, logFile)

		case "assistant":
			handleAssistantEvent(&event)

		case "user":
			handleUserEvent(&event, &result)

		case "result":
			handleResultEvent(&event, &result)
		}
	}

	if err := scanner.Err(); err != nil {
		return &result, fmt.Errorf("reading stream: %w", err)
	}

	result.Text = textBuf.String()
	return &result, nil
}

// streamEvent is the top-level JSON structure from stream-json output.
type streamEvent struct {
	Type      string          `json:"type"`
	Event     json.RawMessage `json:"event"`
	Message   json.RawMessage `json:"message"`
	SessionID string          `json:"session_id"`

	// Fields for "result" type
	Result    json.RawMessage `json:"result"`
	CostUSD   float64         `json:"cost_usd"`

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
	Type         string         `json:"type"`
	ContentBlock *contentBlock  `json:"content_block"`
	Delta        *deltaBlock    `json:"delta"`
}

// deltaBlock holds the delta in content_block_delta events.
type deltaBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// resultPayload is the inner result object from the final result event.
type resultPayload struct {
	PermissionDenials []permDenialEntry `json:"permission_denials"`
	CostUSD           float64           `json:"cost_usd"`
	SessionID         string            `json:"session_id"`
}

type permDenialEntry struct {
	ToolName string `json:"tool_name"`
	Input    string `json:"input"`
}

func handleStreamEvent(event *streamEvent, textBuf *strings.Builder, display io.Writer, logFile io.Writer) {
	if event.Event == nil {
		return
	}

	var nested nestedEvent
	if err := json.Unmarshal(event.Event, &nested); err != nil {
		return
	}

	switch nested.Type {
	case "content_block_delta":
		if nested.Delta != nil && nested.Delta.Type == "text_delta" {
			text := nested.Delta.Text
			textBuf.WriteString(text)
			if display != nil {
				fmt.Fprint(display, text)
			}
			if logFile != nil {
				fmt.Fprint(logFile, text)
			}
		}

	case "content_block_start":
		if nested.ContentBlock != nil && nested.ContentBlock.Type == "tool_use" {
			inputStr := ""
			if nested.ContentBlock.Input != nil {
				inputStr = string(nested.ContentBlock.Input)
			}
			ux.ToolUse(nested.ContentBlock.Name, inputStr)
		}
	}
}

func handleAssistantEvent(event *streamEvent) {
	// Assistant events contain complete tool_use blocks.
	// We display these inline via stream_event content_block_start,
	// so nothing additional needed here.
}

func handleUserEvent(event *streamEvent, result *StreamResult) {
	if event.IsError {
		// Permission denial or other error from the user side
		for _, block := range event.Content {
			if strings.Contains(block.Text, "permission") || strings.Contains(block.Text, "denied") {
				// The actual denials are in the result event; this is just a signal
			}
		}
	}
}

func handleResultEvent(event *streamEvent, result *StreamResult) {
	// Try to parse the nested result object
	if event.Result != nil {
		var payload resultPayload
		if err := json.Unmarshal(event.Result, &payload); err == nil {
			result.CostUSD = payload.CostUSD
			result.SessionID = payload.SessionID
			for _, d := range payload.PermissionDenials {
				result.PermissionDenials = append(result.PermissionDenials, PermissionDenial{
					Tool:  d.ToolName,
					Input: d.Input,
				})
			}
			return
		}
	}

	// Fallback: cost might be at top level
	if event.CostUSD > 0 {
		result.CostUSD = event.CostUSD
	}
	if event.SessionID != "" {
		result.SessionID = event.SessionID
	}
}
