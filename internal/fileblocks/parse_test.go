package fileblocks

import (
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
