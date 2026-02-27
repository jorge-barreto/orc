package contextgather

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const maxFileSize = 32 * 1024 // 32KB per file

// skipDirs are directories excluded from the tree listing.
var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	".venv":        true,
	"__pycache__":  true,
	".orc":         true,
}

// wellKnownFiles are probed for content (relative to project root).
var wellKnownFiles = []string{
	"README.md",
	"readme.md",
	"README",
	"Makefile",
	"makefile",
	"package.json",
	"go.mod",
	"pyproject.toml",
	"setup.py",
	"requirements.txt",
	"Cargo.toml",
	"CLAUDE.md",
	".cursorrules",
}

// wellKnownGlobs are glob patterns probed for content.
var wellKnownGlobs = []string{
	".github/workflows/*.yml",
	".github/workflows/*.yaml",
}

// ProjectContext holds gathered project information.
type ProjectContext struct {
	DirTree string            // top-level + one level deep listing
	Files   map[string]string // relative path -> contents
	GitLog  string            // last 10 commits
}

// Gather collects project context from the given directory.
func Gather(projectRoot string) (*ProjectContext, error) {
	pc := &ProjectContext{
		Files: make(map[string]string),
	}

	pc.DirTree = buildTree(projectRoot)
	gatherFiles(projectRoot, pc)
	pc.GitLog = gatherGitLog(projectRoot)

	return pc, nil
}

// Render formats the context as a prompt section.
func (pc *ProjectContext) Render() string {
	var buf strings.Builder

	buf.WriteString("## Project Directory Structure\n\n```\n")
	buf.WriteString(pc.DirTree)
	buf.WriteString("```\n")

	if len(pc.Files) > 0 {
		buf.WriteString("\n## Key Files\n")

		// Sort paths for deterministic output
		paths := make([]string, 0, len(pc.Files))
		for p := range pc.Files {
			paths = append(paths, p)
		}
		sort.Strings(paths)

		for _, p := range paths {
			buf.WriteString(fmt.Sprintf("\n### %s\n\n```\n%s\n```\n", p, pc.Files[p]))
		}
	}

	if pc.GitLog != "" {
		buf.WriteString("\n## Recent Git History\n\n```\n")
		buf.WriteString(pc.GitLog)
		buf.WriteString("```\n")
	}

	return buf.String()
}

func buildTree(root string) string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "(unable to read directory)\n"
	}

	var buf strings.Builder
	for _, e := range entries {
		if skipDirs[e.Name()] {
			continue
		}
		if e.IsDir() {
			buf.WriteString(e.Name() + "/\n")
			// One level deeper
			subPath := filepath.Join(root, e.Name())
			subEntries, err := os.ReadDir(subPath)
			if err != nil {
				continue
			}
			for _, se := range subEntries {
				if se.IsDir() {
					buf.WriteString("  " + se.Name() + "/\n")
				} else {
					buf.WriteString("  " + se.Name() + "\n")
				}
			}
		} else {
			buf.WriteString(e.Name() + "\n")
		}
	}
	return buf.String()
}

func gatherFiles(root string, pc *ProjectContext) {
	// Direct file probes
	for _, name := range wellKnownFiles {
		path := filepath.Join(root, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := string(data)
		if len(content) > maxFileSize {
			content = content[:maxFileSize] + "\n... (truncated)"
		}
		pc.Files[name] = content
	}

	// Glob patterns
	for _, pattern := range wellKnownGlobs {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			continue
		}
		for _, match := range matches {
			data, err := os.ReadFile(match)
			if err != nil {
				continue
			}
			rel, err := filepath.Rel(root, match)
			if err != nil {
				continue
			}
			content := string(data)
			if len(content) > maxFileSize {
				content = content[:maxFileSize] + "\n... (truncated)"
			}
			pc.Files[rel] = content
		}
	}
}

func gatherGitLog(root string) string {
	cmd := exec.Command("git", "log", "--oneline", "-10")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
