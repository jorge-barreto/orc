package dispatch

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func streamLines(lines ...string) *bytes.Reader {
	return bytes.NewReader([]byte(strings.Join(lines, "\n") + "\n"))
}

func TestProcessStream_TextDeltas(t *testing.T) {
	input := streamLines(
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":" world"}}}`,
		`{"type":"result","total_cost_usd":0.01,"session_id":"sess-123","usage":{"input_tokens":100,"output_tokens":50},"permission_denials":[]}`,
	)

	var display bytes.Buffer
	var log bytes.Buffer
	result, err := ProcessStream(context.Background(), input, &display, &log, nil)
	if err != nil {
		t.Fatal(err)
	}

	if result.Text != "Hello world" {
		t.Fatalf("Text = %q, want %q", result.Text, "Hello world")
	}
	if display.String() != "Hello world" {
		t.Fatalf("display = %q", display.String())
	}
	if log.String() != "Hello world" {
		t.Fatalf("log = %q", log.String())
	}
	if result.CostUSD != 0.01 {
		t.Fatalf("CostUSD = %f", result.CostUSD)
	}
	if result.SessionID != "sess-123" {
		t.Fatalf("SessionID = %q", result.SessionID)
	}
}

func TestProcessStream_ToolUseEvent(t *testing.T) {
	input := streamLines(
		// Tool use: content_block_start -> input_json_deltas -> content_block_stop
		`{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"tool_use","name":"Read","input":{}}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{\"file_path\":\""}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"src/main.go\""}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"}"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop"}}`,
		// Then some text output
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Done"}}}`,
		`{"type":"result","total_cost_usd":0.05,"session_id":"s1","usage":{"input_tokens":200,"output_tokens":100},"permission_denials":[]}`,
	)

	var display bytes.Buffer
	result, err := ProcessStream(context.Background(), input, &display, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "Done" {
		t.Fatalf("Text = %q", result.Text)
	}
}

func TestProcessStream_PermissionDenials(t *testing.T) {
	input := streamLines(
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"trying..."}}}`,
		`{"type":"user","is_error":true,"content":[{"type":"text","text":"permission denied"}]}`,
		`{"type":"result","total_cost_usd":0.02,"session_id":"s2","usage":{"input_tokens":300,"output_tokens":150},"permission_denials":[{"tool_name":"Bash","input":"docker compose up"},{"tool_name":"Read","input":"/etc/shadow"}]}`,
	)

	result, err := ProcessStream(context.Background(), input, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.PermissionDenials) != 2 {
		t.Fatalf("got %d denials, want 2", len(result.PermissionDenials))
	}
	if result.PermissionDenials[0].Tool != "Bash" {
		t.Fatalf("denial[0].Tool = %q", result.PermissionDenials[0].Tool)
	}
	if result.PermissionDenials[0].Input != "docker compose up" {
		t.Fatalf("denial[0].Input = %q", result.PermissionDenials[0].Input)
	}
	if result.PermissionDenials[1].Tool != "Read" {
		t.Fatalf("denial[1].Tool = %q", result.PermissionDenials[1].Tool)
	}
}

func TestProcessStream_EmptyStream(t *testing.T) {
	input := streamLines()
	result, err := ProcessStream(context.Background(), input, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "" {
		t.Fatalf("Text = %q, want empty", result.Text)
	}
	if len(result.PermissionDenials) != 0 {
		t.Fatalf("got %d denials, want 0", len(result.PermissionDenials))
	}
}

func TestProcessStream_MalformedJSON(t *testing.T) {
	input := streamLines(
		`not json at all`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"ok"}}}`,
		`{broken`,
		`{"type":"result","total_cost_usd":0.01,"session_id":"s3","usage":{"input_tokens":50,"output_tokens":25},"permission_denials":[]}`,
	)

	result, err := ProcessStream(context.Background(), input, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "ok" {
		t.Fatalf("Text = %q, want %q", result.Text, "ok")
	}
}

func TestProcessStream_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	input := streamLines(
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}}`,
	)

	_, err := ProcessStream(ctx, input, nil, nil, nil)
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestProcessStream_ResultCostAndTokens(t *testing.T) {
	input := streamLines(
		`{"type":"result","total_cost_usd":0.03,"session_id":"s4","usage":{"input_tokens":500,"output_tokens":200,"cache_creation_input_tokens":1000,"cache_read_input_tokens":4000},"permission_denials":[]}`,
	)

	result, err := ProcessStream(context.Background(), input, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.CostUSD != 0.03 {
		t.Fatalf("CostUSD = %f", result.CostUSD)
	}
	if result.SessionID != "s4" {
		t.Fatalf("SessionID = %q", result.SessionID)
	}
	if result.InputTokens != 500 {
		t.Fatalf("InputTokens = %d, want 500", result.InputTokens)
	}
	if result.OutputTokens != 200 {
		t.Fatalf("OutputTokens = %d, want 200", result.OutputTokens)
	}
	if result.CacheCreationInputTokens != 1000 {
		t.Fatalf("CacheCreationInputTokens = %d, want 1000", result.CacheCreationInputTokens)
	}
	if result.CacheReadInputTokens != 4000 {
		t.Fatalf("CacheReadInputTokens = %d, want 4000", result.CacheReadInputTokens)
	}
}

func TestProcessStream_NilWriters(t *testing.T) {
	input := streamLines(
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}}`,
		`{"type":"result","total_cost_usd":0.01,"session_id":"s5","usage":{"input_tokens":10,"output_tokens":5},"permission_denials":[]}`,
	)

	result, err := ProcessStream(context.Background(), input, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "Hello" {
		t.Fatalf("Text = %q", result.Text)
	}
}

func TestProcessStream_MultiToolStreaming(t *testing.T) {
	input := streamLines(
		// First tool: Bash
		`{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"tool_use","name":"Bash","input":{}}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{\"command\":\"ls -la\"}"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop"}}`,
		// Text between tools
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Checking files..."}}}`,
		// Second tool: Grep
		`{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"tool_use","name":"Grep","input":{}}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{\"pattern\""}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":":\"TODO\"}"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop"}}`,
		`{"type":"result","total_cost_usd":0.10,"session_id":"s6","usage":{"input_tokens":500,"output_tokens":250},"permission_denials":[]}`,
	)

	var display bytes.Buffer
	result, err := ProcessStream(context.Background(), input, &display, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "Checking files..." {
		t.Fatalf("Text = %q, want %q", result.Text, "Checking files...")
	}
	if result.CostUSD != 0.10 {
		t.Fatalf("CostUSD = %f", result.CostUSD)
	}
	// Display should contain a newline between text and the next tool block
	out := display.String()
	if !strings.Contains(out, "Checking files...\n") {
		t.Fatalf("display missing newline after text before tool use: %q", out)
	}
}

func TestProcessStream_AskUserQuestion(t *testing.T) {
	input := streamLines(
		`{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"tool_use","name":"AskUserQuestion","input":{}}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{\"question\":\"Should I refresh SSO?\",\"options\":[\"Yes\",\"No\",\"Skip\"]}"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop"}}`,
		`{"type":"result","total_cost_usd":0.01,"session_id":"s1","usage":{"input_tokens":100,"output_tokens":50},"permission_denials":[]}`,
	)

	result, err := ProcessStream(context.Background(), input, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.UserQuestions) != 1 {
		t.Fatalf("got %d user questions, want 1", len(result.UserQuestions))
	}
	if result.UserQuestions[0].Question != "Should I refresh SSO?" {
		t.Fatalf("Question = %q", result.UserQuestions[0].Question)
	}
	if len(result.UserQuestions[0].Options) != 3 {
		t.Fatalf("got %d options, want 3", len(result.UserQuestions[0].Options))
	}
	if result.UserQuestions[0].Options[0] != "Yes" || result.UserQuestions[0].Options[1] != "No" || result.UserQuestions[0].Options[2] != "Skip" {
		t.Fatalf("Options = %v", result.UserQuestions[0].Options)
	}
}

func TestProcessStream_AskUserQuestionNoOptions(t *testing.T) {
	input := streamLines(
		`{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"tool_use","name":"AskUserQuestion","input":{}}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{\"question\":\"What should I do next?\",\"options\":[]}"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop"}}`,
		`{"type":"result","total_cost_usd":0.01,"session_id":"s1","usage":{"input_tokens":100,"output_tokens":50},"permission_denials":[]}`,
	)

	result, err := ProcessStream(context.Background(), input, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.UserQuestions) != 1 {
		t.Fatalf("got %d user questions, want 1", len(result.UserQuestions))
	}
	if result.UserQuestions[0].Question != "What should I do next?" {
		t.Fatalf("Question = %q", result.UserQuestions[0].Question)
	}
	if len(result.UserQuestions[0].Options) != 0 {
		t.Fatalf("got %d options, want 0", len(result.UserQuestions[0].Options))
	}
}

func TestProcessStream_AskUserQuestionMalformedJSON(t *testing.T) {
	input := streamLines(
		`{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"tool_use","name":"AskUserQuestion","input":{}}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{broken json"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop"}}`,
		`{"type":"result","total_cost_usd":0.01,"session_id":"s1","usage":{"input_tokens":100,"output_tokens":50},"permission_denials":[]}`,
	)

	result, err := ProcessStream(context.Background(), input, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.UserQuestions) != 0 {
		t.Fatalf("got %d user questions, want 0 (malformed JSON should be skipped)", len(result.UserQuestions))
	}
}

func TestProcessStream_MultipleAskUserQuestions(t *testing.T) {
	input := streamLines(
		// First AskUserQuestion
		`{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"tool_use","name":"AskUserQuestion","input":{}}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{\"question\":\"First question?\",\"options\":[\"A\",\"B\"]}"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop"}}`,
		// Second AskUserQuestion
		`{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"tool_use","name":"AskUserQuestion","input":{}}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{\"question\":\"Second question?\",\"options\":[]}"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop"}}`,
		`{"type":"result","total_cost_usd":0.01,"session_id":"s1","usage":{"input_tokens":100,"output_tokens":50},"permission_denials":[]}`,
	)

	result, err := ProcessStream(context.Background(), input, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.UserQuestions) != 2 {
		t.Fatalf("got %d user questions, want 2", len(result.UserQuestions))
	}
	if result.UserQuestions[0].Question != "First question?" {
		t.Fatalf("Question[0] = %q", result.UserQuestions[0].Question)
	}
	if result.UserQuestions[1].Question != "Second question?" {
		t.Fatalf("Question[1] = %q", result.UserQuestions[1].Question)
	}
}

func TestToolUseSummary(t *testing.T) {
	tests := []struct {
		name    string
		tool    string
		rawJSON string
		want    string
	}{
		{"Bash command", "Bash", `{"command":"ls -la"}`, "ls -la"},
		{"Read file_path", "Read", `{"file_path":"/tmp/foo.go"}`, "/tmp/foo.go"},
		{"Write file_path", "Write", `{"file_path":"out.txt","content":"hello"}`, "out.txt"},
		{"Edit file_path", "Edit", `{"file_path":"main.go","old_string":"a","new_string":"b"}`, "main.go"},
		{"Grep pattern", "Grep", `{"pattern":"TODO","path":"."}`, "TODO"},
		{"Glob pattern", "Glob", `{"pattern":"**/*.go"}`, "**/*.go"},
		{"Task description", "Task", `{"description":"search code"}`, "search code"},
		{"TaskCreate description", "TaskCreate", `{"description":"fix bug","subject":"bug"}`, "fix bug"},
		{"AskUserQuestion question", "AskUserQuestion", `{"question":"Refresh SSO?","options":["Yes","No"]}`, "Refresh SSO?"},
		{"Unknown tool first string", "WebSearch", `{"query":"golang"}`, "golang"},
		{"Empty input", "Bash", "", ""},
		{"Malformed JSON", "Bash", "{broken", "{broken"},
		{"Missing key fallback", "Bash", `{"timeout":30}`, `{"timeout":30}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toolUseSummary(tt.tool, tt.rawJSON)
			if got != tt.want {
				t.Errorf("toolUseSummary(%q, %q) = %q, want %q", tt.tool, tt.rawJSON, got, tt.want)
			}
		})
	}
}

func TestPermissionDenial_String(t *testing.T) {
	d := PermissionDenial{Tool: "Bash", Input: "rm -rf /"}
	if s := d.String(); s != "Bash(rm -rf /)" {
		t.Fatalf("got %q", s)
	}
	d2 := PermissionDenial{Tool: "Read"}
	if s := d2.String(); s != "Read" {
		t.Fatalf("got %q", s)
	}
}

func TestProcessStream_FullTokenCounts(t *testing.T) {
	input := streamLines(
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}}`,
		`{"type":"result","total_cost_usd":0.05,"session_id":"s1","usage":{"input_tokens":1500,"output_tokens":800,"cache_creation_input_tokens":3000,"cache_read_input_tokens":10000},"permission_denials":[]}`,
	)

	result, err := ProcessStream(context.Background(), input, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.InputTokens != 1500 {
		t.Fatalf("InputTokens = %d, want 1500", result.InputTokens)
	}
	if result.OutputTokens != 800 {
		t.Fatalf("OutputTokens = %d, want 800", result.OutputTokens)
	}
	if result.CacheCreationInputTokens != 3000 {
		t.Fatalf("CacheCreationInputTokens = %d, want 3000", result.CacheCreationInputTokens)
	}
	if result.CacheReadInputTokens != 10000 {
		t.Fatalf("CacheReadInputTokens = %d, want 10000", result.CacheReadInputTokens)
	}
	if result.CostUSD != 0.05 {
		t.Fatalf("CostUSD = %f, want 0.05", result.CostUSD)
	}
}

func TestProcessStream_NoUsageObject(t *testing.T) {
	input := streamLines(
		`{"type":"result","total_cost_usd":0,"session_id":"s1","permission_denials":[]}`,
	)

	result, err := ProcessStream(context.Background(), input, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.InputTokens != 0 {
		t.Fatalf("InputTokens = %d, want 0", result.InputTokens)
	}
	if result.OutputTokens != 0 {
		t.Fatalf("OutputTokens = %d, want 0", result.OutputTokens)
	}
	if result.CacheCreationInputTokens != 0 {
		t.Fatalf("CacheCreationInputTokens = %d, want 0", result.CacheCreationInputTokens)
	}
	if result.CacheReadInputTokens != 0 {
		t.Fatalf("CacheReadInputTokens = %d, want 0", result.CacheReadInputTokens)
	}
}

func TestProcessStream_UsageWithoutCacheFields(t *testing.T) {
	input := streamLines(
		`{"type":"result","total_cost_usd":0.03,"session_id":"s1","usage":{"input_tokens":2000,"output_tokens":1000},"permission_denials":[]}`,
	)

	result, err := ProcessStream(context.Background(), input, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.InputTokens != 2000 {
		t.Fatalf("InputTokens = %d, want 2000", result.InputTokens)
	}
	if result.OutputTokens != 1000 {
		t.Fatalf("OutputTokens = %d, want 1000", result.OutputTokens)
	}
	if result.CacheCreationInputTokens != 0 {
		t.Fatalf("CacheCreationInputTokens = %d, want 0", result.CacheCreationInputTokens)
	}
	if result.CacheReadInputTokens != 0 {
		t.Fatalf("CacheReadInputTokens = %d, want 0", result.CacheReadInputTokens)
	}
}

func TestProcessStream_MultipleResultEventsPreserveUsage(t *testing.T) {
	input := streamLines(
		`{"type":"result","total_cost_usd":0.05,"session_id":"s1","usage":{"input_tokens":1500,"output_tokens":800,"cache_creation_input_tokens":3000,"cache_read_input_tokens":10000},"permission_denials":[]}`,
		`{"type":"result","total_cost_usd":0,"session_id":"s1","usage":{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0},"permission_denials":[]}`,
	)

	result, err := ProcessStream(context.Background(), input, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.CostUSD != 0.05 {
		t.Fatalf("CostUSD = %f, want 0.05", result.CostUSD)
	}
	if result.InputTokens != 1500 {
		t.Fatalf("InputTokens = %d, want 1500", result.InputTokens)
	}
	if result.OutputTokens != 800 {
		t.Fatalf("OutputTokens = %d, want 800", result.OutputTokens)
	}
	if result.CacheCreationInputTokens != 3000 {
		t.Fatalf("CacheCreationInputTokens = %d, want 3000", result.CacheCreationInputTokens)
	}
	if result.CacheReadInputTokens != 10000 {
		t.Fatalf("CacheReadInputTokens = %d, want 10000", result.CacheReadInputTokens)
	}
}

func TestProcessStream_MultipleResultEventsUpdatesHigherValues(t *testing.T) {
	input := streamLines(
		`{"type":"result","total_cost_usd":0.02,"session_id":"s1","usage":{"input_tokens":500,"output_tokens":200},"permission_denials":[]}`,
		`{"type":"result","total_cost_usd":0.08,"session_id":"s1","usage":{"input_tokens":2000,"output_tokens":1000},"permission_denials":[]}`,
	)

	result, err := ProcessStream(context.Background(), input, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.CostUSD != 0.08 {
		t.Fatalf("CostUSD = %f, want 0.08", result.CostUSD)
	}
	if result.InputTokens != 2000 {
		t.Fatalf("InputTokens = %d, want 2000", result.InputTokens)
	}
	if result.OutputTokens != 1000 {
		t.Fatalf("OutputTokens = %d, want 1000", result.OutputTokens)
	}
}

func TestProcessStream_RawLogReceivesAllLines(t *testing.T) {
	lines := []string{
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}}`,
		``,
		`{"type":"result","total_cost_usd":0.01,"session_id":"s1","usage":{"input_tokens":100,"output_tokens":50},"permission_denials":[]}`,
	}
	input := streamLines(lines...)

	var rawLog bytes.Buffer
	result, err := ProcessStream(context.Background(), input, nil, nil, &rawLog)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "Hello" {
		t.Fatalf("Text = %q, want %q", result.Text, "Hello")
	}

	// rawLog should contain all lines including the empty one, each followed by a newline
	rawOutput := rawLog.String()
	for _, line := range lines {
		if line == "" {
			// Empty line should appear as a bare newline
			if !strings.Contains(rawOutput, "\n\n") {
				t.Fatalf("rawLog missing empty line; got:\n%s", rawOutput)
			}
			continue
		}
		if !strings.Contains(rawOutput, line) {
			t.Fatalf("rawLog missing line %q; got:\n%s", line, rawOutput)
		}
	}
	// Verify line count: 3 lines (2 JSON + 1 empty), each with a trailing newline
	rawLines := strings.Split(strings.TrimRight(rawOutput, "\n"), "\n")
	if len(rawLines) != 3 {
		t.Fatalf("expected 3 raw lines, got %d: %v", len(rawLines), rawLines)
	}
}

func TestProcessStream_NilRawLog(t *testing.T) {
	input := streamLines(
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"ok"}}}`,
		`{"type":"result","total_cost_usd":0.01,"session_id":"s1","usage":{"input_tokens":10,"output_tokens":5},"permission_denials":[]}`,
	)

	result, err := ProcessStream(context.Background(), input, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "ok" {
		t.Fatalf("Text = %q", result.Text)
	}
}

func TestProcessStream_ToolsUsed(t *testing.T) {
	input := streamLines(
		// Bash (first use)
		`{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"tool_use","name":"Bash","input":{}}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{\"command\":\"ls\"}"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop"}}`,
		// Read
		`{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"tool_use","name":"Read","input":{}}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{\"file_path\":\"main.go\"}"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop"}}`,
		// Bash (second use — should be deduplicated)
		`{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"tool_use","name":"Bash","input":{}}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{\"command\":\"pwd\"}"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop"}}`,
		`{"type":"result","total_cost_usd":0.01,"session_id":"s1","usage":{"input_tokens":100,"output_tokens":50},"permission_denials":[]}`,
	)

	result, err := ProcessStream(context.Background(), input, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolsUsed) != 2 {
		t.Fatalf("ToolsUsed = %v, want [Bash Read]", result.ToolsUsed)
	}
	if result.ToolsUsed[0] != "Bash" {
		t.Fatalf("ToolsUsed[0] = %q, want %q", result.ToolsUsed[0], "Bash")
	}
	if result.ToolsUsed[1] != "Read" {
		t.Fatalf("ToolsUsed[1] = %q, want %q", result.ToolsUsed[1], "Read")
	}
}

func TestProcessStream_ToolsUsedEmpty(t *testing.T) {
	input := streamLines(
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"just text"}}}`,
		`{"type":"result","total_cost_usd":0.01,"session_id":"s1","usage":{"input_tokens":100,"output_tokens":50},"permission_denials":[]}`,
	)

	result, err := ProcessStream(context.Background(), input, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolsUsed) != 0 {
		t.Fatalf("ToolsUsed = %v, want empty", result.ToolsUsed)
	}
}

func TestProcessStream_ToolsUsedExcludesAskUserQuestion(t *testing.T) {
	input := streamLines(
		// AskUserQuestion — should be excluded from ToolsUsed
		`{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"tool_use","name":"AskUserQuestion","input":{}}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{\"question\":\"Continue?\",\"options\":[\"Yes\",\"No\"]}"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop"}}`,
		// Bash — should appear in ToolsUsed
		`{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"tool_use","name":"Bash","input":{}}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{\"command\":\"ls\"}"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop"}}`,
		`{"type":"result","total_cost_usd":0.01,"session_id":"s1","usage":{"input_tokens":100,"output_tokens":50},"permission_denials":[]}`,
	)

	result, err := ProcessStream(context.Background(), input, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolsUsed) != 1 {
		t.Fatalf("ToolsUsed = %v, want [Bash]", result.ToolsUsed)
	}
	if result.ToolsUsed[0] != "Bash" {
		t.Fatalf("ToolsUsed[0] = %q, want %q", result.ToolsUsed[0], "Bash")
	}
	if len(result.UserQuestions) != 1 {
		t.Fatalf("UserQuestions len = %d, want 1", len(result.UserQuestions))
	}
}

// TestProcessStream_RateLimitEvent_RealWireFormat uses the actual JSON shape
// emitted by production claude — rate_limit_info at the TOP LEVEL of the event,
// not wrapped inside a nested "event" field. This guards against the bug where
// every previous fixture had it nested and the parser never worked against
// real traffic.
func TestProcessStream_RateLimitEvent_RealWireFormat(t *testing.T) {
	input := streamLines(
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"working..."}}}`,
		`{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","resetsAt":1712900000,"rateLimitType":"five_hour","overageStatus":"rejected","overageDisabledReason":"org_level_disabled","isUsingOverage":false},"uuid":"u1","session_id":"s1"}`,
		`{"type":"result","total_cost_usd":0,"session_id":"s1","is_error":true,"api_error_status":429,"usage":{"input_tokens":100,"output_tokens":50},"permission_denials":[]}`,
	)
	result, err := ProcessStream(context.Background(), input, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.RateLimited {
		t.Fatal("RateLimited = false, want true (real-wire rate_limit_info at top level)")
	}
	if result.RateLimitResetAt != 1712900000 {
		t.Fatalf("RateLimitResetAt = %d, want 1712900000", result.RateLimitResetAt)
	}
}

func TestProcessStream_RateLimitEvent(t *testing.T) {
	input := streamLines(
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"working..."}}}`,
		`{"type":"rate_limit_event","event":{"rate_limit_info":{"status":"rejected","resetsAt":1712900000}}}`,
		`{"type":"result","total_cost_usd":0,"session_id":"s1","is_error":true,"usage":{"input_tokens":100,"output_tokens":50},"permission_denials":[]}`,
	)

	result, err := ProcessStream(context.Background(), input, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.RateLimited {
		t.Fatal("RateLimited = false, want true")
	}
	if result.RateLimitResetAt != 1712900000 {
		t.Fatalf("RateLimitResetAt = %d, want 1712900000", result.RateLimitResetAt)
	}
}

func TestProcessStream_RateLimitEventNotRejected(t *testing.T) {
	input := streamLines(
		`{"type":"rate_limit_event","event":{"rate_limit_info":{"status":"allowed","resetsAt":1712900000}}}`,
		`{"type":"result","total_cost_usd":0.01,"session_id":"s1","usage":{"input_tokens":100,"output_tokens":50},"permission_denials":[]}`,
	)

	result, err := ProcessStream(context.Background(), input, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.RateLimited {
		t.Fatal("RateLimited = true, want false for non-rejected status")
	}
}

func TestProcessStream_RateLimitEventMissingResetAt(t *testing.T) {
	input := streamLines(
		`{"type":"rate_limit_event","event":{"rate_limit_info":{"status":"rejected"}}}`,
		`{"type":"result","total_cost_usd":0,"session_id":"s1","usage":{"input_tokens":0,"output_tokens":0},"permission_denials":[]}`,
	)

	result, err := ProcessStream(context.Background(), input, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.RateLimited {
		t.Fatal("RateLimited = false, want true")
	}
	if result.RateLimitResetAt != 0 {
		t.Fatalf("RateLimitResetAt = %d, want 0", result.RateLimitResetAt)
	}
}

func TestWarnWriter(t *testing.T) {
	var buf bytes.Buffer
	ww := &warnWriter{w: &buf}
	n, err := ww.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 5 {
		t.Fatalf("n = %d, want 5", n)
	}
	if buf.String() != "hello" {
		t.Fatalf("buf = %q, want %q", buf.String(), "hello")
	}
}

func TestWarnWriter_WarnsOnce(t *testing.T) {
	ww := &warnWriter{w: errWriter{}}
	n, err := ww.Write([]byte("first"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 5 {
		t.Fatalf("n = %d, want 5", n)
	}
	if !ww.failed {
		t.Fatal("expected failed = true after write error")
	}
	n2, err2 := ww.Write([]byte("second"))
	if err2 != nil {
		t.Fatalf("unexpected error on second write: %v", err2)
	}
	if n2 != 6 {
		t.Fatalf("n = %d, want 6", n2)
	}
}

func TestProcessStream_RawLogWriteError(t *testing.T) {
	input := streamLines(
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}}`,
		`{"type":"result","total_cost_usd":0.01,"session_id":"s1","usage":{"input_tokens":100,"output_tokens":50},"permission_denials":[]}`,
	)
	result, err := ProcessStream(context.Background(), input, nil, nil, errWriter{})
	if err != nil {
		t.Fatalf("ProcessStream failed: %v", err)
	}
	if result.Text != "Hello" {
		t.Fatalf("Text = %q, want %q", result.Text, "Hello")
	}
	if result.CostUSD != 0.01 {
		t.Fatalf("CostUSD = %f, want 0.01", result.CostUSD)
	}
}
