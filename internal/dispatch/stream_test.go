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
		`{"type":"result","result":{"cost_usd":0.01,"session_id":"sess-123"}}`,
	)

	var display bytes.Buffer
	var log bytes.Buffer
	result, err := processStream(context.Background(), input, &display, &log)
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
		`{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"tool_use","name":"Read","input":"src/main.go"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Done"}}}`,
		`{"type":"result","result":{"cost_usd":0.05,"session_id":"s1"}}`,
	)

	var display bytes.Buffer
	result, err := processStream(context.Background(), input, &display, nil)
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
		`{"type":"result","result":{"cost_usd":0.02,"session_id":"s2","permission_denials":[{"tool_name":"Bash","input":"docker compose up"},{"tool_name":"Read","input":"/etc/shadow"}]}}`,
	)

	result, err := processStream(context.Background(), input, nil, nil)
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
	result, err := processStream(context.Background(), input, nil, nil)
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
		`{"type":"result","result":{"cost_usd":0.01,"session_id":"s3"}}`,
	)

	result, err := processStream(context.Background(), input, nil, nil)
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

	_, err := processStream(ctx, input, nil, nil)
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestProcessStream_ResultTopLevelCost(t *testing.T) {
	input := streamLines(
		`{"type":"result","cost_usd":0.03,"session_id":"s4"}`,
	)

	result, err := processStream(context.Background(), input, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.CostUSD != 0.03 {
		t.Fatalf("CostUSD = %f", result.CostUSD)
	}
	if result.SessionID != "s4" {
		t.Fatalf("SessionID = %q", result.SessionID)
	}
}

func TestProcessStream_NilWriters(t *testing.T) {
	input := streamLines(
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}}`,
		`{"type":"result","result":{"cost_usd":0.01,"session_id":"s5"}}`,
	)

	result, err := processStream(context.Background(), input, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "Hello" {
		t.Fatalf("Text = %q", result.Text)
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
