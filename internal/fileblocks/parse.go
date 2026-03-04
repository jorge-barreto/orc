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

// fenceOpenRe captures the backtick run and the file= path.
// Group 1: backticks (3+), Group 2: file path.
var fenceOpenRe = regexp.MustCompile("^(`{3,})\\w*\\s*file=(\\S+)")

// backtickOnly matches lines that are nothing but backticks (3+).
var backtickOnly = regexp.MustCompile("^`{3,}$")

// Parse extracts fenced code blocks annotated with file= from text.
// It recognizes opening fences like:
//
//	```yaml file=.orc/config.yaml
//	````markdown file=.orc/phases/plan.md
//
// The closing fence must be a line of only backticks with length >= the
// opening fence, following the CommonMark spec. This allows content to
// contain shorter fences without prematurely closing the block.
//
// Returns blocks in order of appearance.
func Parse(text string) []FileBlock {
	lines := strings.Split(text, "\n")
	var blocks []FileBlock
	var current *FileBlock
	var fenceLen int
	var buf strings.Builder

	for _, line := range lines {
		if current != nil {
			// Inside a block — look for closing fence
			trimmed := strings.TrimSpace(line)
			if backtickOnly.MatchString(trimmed) && len(trimmed) >= fenceLen {
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
			fenceLen = len(m[1])
			current = &FileBlock{Path: m[2]}
			buf.Reset()
		}
	}

	return blocks
}
