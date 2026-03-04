package fileblocks

import (
	"strings"
	"testing"
)

func TestParse_SingleBlock(t *testing.T) {
	input := "```yaml file=.orc/config.yaml\nname: test\nphases: []\n```\n"
	blocks := Parse(input)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Path != ".orc/config.yaml" {
		t.Fatalf("expected path .orc/config.yaml, got %q", blocks[0].Path)
	}
	if blocks[0].Content != "name: test\nphases: []" {
		t.Fatalf("unexpected content: %q", blocks[0].Content)
	}
}

func TestParse_MultipleBlocks(t *testing.T) {
	input := `Some text before

` + "```yaml file=.orc/config.yaml" + `
name: test
` + "```" + `

More text

` + "```markdown file=.orc/phases/plan.md" + `
You are working on $TICKET.
` + "```" + `
`
	blocks := Parse(input)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0].Path != ".orc/config.yaml" {
		t.Fatalf("block 0: expected path .orc/config.yaml, got %q", blocks[0].Path)
	}
	if blocks[1].Path != ".orc/phases/plan.md" {
		t.Fatalf("block 1: expected path .orc/phases/plan.md, got %q", blocks[1].Path)
	}
}

func TestParse_NoFileAnnotation_Skipped(t *testing.T) {
	input := "```yaml\nname: test\n```\n"
	blocks := Parse(input)
	if len(blocks) != 0 {
		t.Fatalf("expected 0 blocks, got %d", len(blocks))
	}
}

func TestParse_NoLanguageTag(t *testing.T) {
	input := "```file=.orc/config.yaml\ncontent here\n```\n"
	blocks := Parse(input)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Path != ".orc/config.yaml" {
		t.Fatalf("expected path .orc/config.yaml, got %q", blocks[0].Path)
	}
}

func TestParse_EmptyContent(t *testing.T) {
	input := "```yaml file=.orc/empty.yaml\n```\n"
	blocks := Parse(input)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Content != "" {
		t.Fatalf("expected empty content, got %q", blocks[0].Content)
	}
}

func TestParse_UnclosedBlock_Dropped(t *testing.T) {
	input := "```yaml file=.orc/config.yaml\nname: test\n"
	blocks := Parse(input)
	if len(blocks) != 0 {
		t.Fatalf("expected 0 blocks for unclosed fence, got %d", len(blocks))
	}
}

func TestParse_MixedAnnotatedAndPlain(t *testing.T) {
	input := "```go\nfunc main() {}\n```\n\n```yaml file=.orc/config.yaml\nname: test\n```\n"
	blocks := Parse(input)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Path != ".orc/config.yaml" {
		t.Fatalf("expected path .orc/config.yaml, got %q", blocks[0].Path)
	}
}

func TestParse_EmbeddedTripleBackticks(t *testing.T) {
	// A 4-backtick file block containing embedded triple-backtick fences.
	// The embedded ``` must NOT close the outer block.
	input := "````markdown file=.orc/phases/plan.md\n" +
		"# Plan\n" +
		"\n" +
		"Key packages:\n" +
		"```\n" +
		"cmd/orc/main.go\n" +
		"internal/config/\n" +
		"```\n" +
		"\n" +
		"## Steps\n" +
		"1. Do the thing\n" +
		"````\n"
	blocks := Parse(input)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if !strings.Contains(blocks[0].Content, "## Steps") {
		t.Fatalf("embedded fence closed block prematurely; content: %q", blocks[0].Content)
	}
	if !strings.Contains(blocks[0].Content, "```") {
		t.Fatalf("embedded fences should be preserved in content; content: %q", blocks[0].Content)
	}
}

func TestParse_FourBacktickCloseRequiresFour(t *testing.T) {
	// A 4-backtick block must not close on 3 backticks.
	input := "````yaml file=.orc/config.yaml\nname: test\n```\nstill here\n````\n"
	blocks := Parse(input)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if !strings.Contains(blocks[0].Content, "still here") {
		t.Fatalf("3-backtick line should not close 4-backtick block; content: %q", blocks[0].Content)
	}
}

func TestParse_FiveBacktickCloseFourBacktickBlock(t *testing.T) {
	// A closing fence with MORE backticks than the opening should also close.
	input := "````yaml file=.orc/config.yaml\nname: test\n`````\n"
	blocks := Parse(input)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Content != "name: test" {
		t.Fatalf("unexpected content: %q", blocks[0].Content)
	}
}
