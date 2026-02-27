package fileblocks

import (
	"regexp"
	"strings"
)

// FileBlock represents a single extracted file from LLM output.
type FileBlock struct {
	Path    string // e.g. ".orc/config.yaml"
	Content string // content between the fences
}

var fenceOpenRe = regexp.MustCompile("^```\\w*\\s*file=(\\S+)")

// Parse extracts fenced code blocks annotated with file= from text.
// It recognizes opening fences like:
//
//	```yaml file=.orc/config.yaml
//	```file=.orc/phases/plan.md
//	```markdown file=.orc/phases/implement.md
//
// Returns blocks in order of appearance.
func Parse(text string) []FileBlock {
	lines := strings.Split(text, "\n")
	var blocks []FileBlock
	var current *FileBlock
	var buf strings.Builder

	for _, line := range lines {
		if current != nil {
			// Inside a block — look for closing fence
			trimmed := strings.TrimSpace(line)
			if trimmed == "```" {
				current.Content = buf.String()
				blocks = append(blocks, *current)
				current = nil
				buf.Reset()
				continue
			}
			if buf.Len() > 0 {
				buf.WriteByte('\n')
			}
			buf.WriteString(line)
			continue
		}

		// Not inside a block — look for opening fence with file=
		m := fenceOpenRe.FindStringSubmatch(strings.TrimSpace(line))
		if m != nil {
			current = &FileBlock{Path: m[1]}
			buf.Reset()
		}
	}

	return blocks
}
