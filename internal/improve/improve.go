package improve

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/dispatch"
	"github.com/jorge-barreto/orc/internal/fileblocks"
	"github.com/jorge-barreto/orc/internal/state"
	"github.com/jorge-barreto/orc/internal/ux"
)

func readOrcFiles(projectRoot string) (configYAML string, phaseFiles map[string]string, err error) {
	configPath := filepath.Join(projectRoot, ".orc", "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		// No config.yaml — check for workflows/
		if _, statErr := os.Stat(filepath.Join(projectRoot, ".orc", "workflows")); statErr != nil {
			return "", nil, fmt.Errorf("no .orc/config.yaml or .orc/workflows/ found — run 'orc init' first")
		}
		configYAML = "" // No default config
	} else {
		configYAML = string(data)
	}

	phaseFiles = make(map[string]string)

	// Read prompt files
	matches, _ := filepath.Glob(filepath.Join(projectRoot, ".orc", "phases", "*.md"))
	for _, match := range matches {
		content, err := os.ReadFile(match)
		if err != nil {
			continue
		}
		relPath, _ := filepath.Rel(projectRoot, match)
		relPath = filepath.ToSlash(relPath)
		phaseFiles[relPath] = string(content)
	}

	// Read workflow configs
	wfMatches, _ := filepath.Glob(filepath.Join(projectRoot, ".orc", "workflows", "*.yaml"))
	for _, match := range wfMatches {
		content, err := os.ReadFile(match)
		if err != nil {
			continue
		}
		relPath, _ := filepath.Rel(projectRoot, match)
		relPath = filepath.ToSlash(relPath)
		phaseFiles[relPath] = string(content)
	}

	return configYAML, phaseFiles, nil
}

func readAuditSummary(projectRoot string) string {
	auditBase := filepath.Join(projectRoot, ".orc", "audit")
	entries, err := os.ReadDir(auditBase)
	if err != nil {
		return ""
	}

	// Collect directories with their start times for sorting by recency.
	type auditEntry struct {
		name  string
		dir   string
		start time.Time
	}
	var audits []auditEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(auditBase, e.Name())

		// Flat layout: audit/<ticket>/
		if isTicketAuditDir(dir) {
			t, tErr := state.LoadTiming(dir)
			var start time.Time
			if tErr == nil && len(t.Entries) > 0 {
				start = t.Entries[0].Start
			}
			if start.IsZero() {
				if info, iErr := e.Info(); iErr == nil {
					start = info.ModTime()
				}
			}
			audits = append(audits, auditEntry{name: e.Name(), dir: dir, start: start})
			continue
		}

		// Workflow-namespaced layout: audit/<workflow>/<ticket>/
		subEntries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, se := range subEntries {
			if !se.IsDir() {
				continue
			}
			ticketDir := filepath.Join(dir, se.Name())
			if !isTicketAuditDir(ticketDir) {
				continue
			}
			t, tErr := state.LoadTiming(ticketDir)
			var start time.Time
			if tErr == nil && len(t.Entries) > 0 {
				start = t.Entries[0].Start
			}
			if start.IsZero() {
				if info, iErr := se.Info(); iErr == nil {
					start = info.ModTime()
				}
			}
			displayName := e.Name() + "/" + se.Name()
			audits = append(audits, auditEntry{name: displayName, dir: ticketDir, start: start})
		}
	}
	sort.Slice(audits, func(i, j int) bool {
		return audits[i].start.After(audits[j].start)
	})
	limit := 5
	if len(audits) < limit {
		limit = len(audits)
	}

	var parts []string
	for _, a := range audits[:limit] {
		ticket := a.name
		auditDir := a.dir

		// Derive artifactsDir: audit/<...> → artifacts/<...>
		relPath, _ := filepath.Rel(auditBase, auditDir)
		artifactsDir := filepath.Join(projectRoot, ".orc", "artifacts", relPath)

		var lines []string

		costs, err := state.LoadCosts(auditDir)
		if err == nil && costs.TotalCostUSD > 0 {
			lines = append(lines, fmt.Sprintf("- Total cost: $%.2f", costs.TotalCostUSD))
			var phaseCosts []string
			for _, p := range costs.Phases {
				phaseCosts = append(phaseCosts, fmt.Sprintf("%s ($%.2f)", p.Name, p.CostUSD))
			}
			if len(phaseCosts) > 0 {
				lines = append(lines, fmt.Sprintf("- Phase costs: %s", strings.Join(phaseCosts, ", ")))
			}
		}

		timing, err := state.LoadTiming(auditDir)
		if err == nil && len(timing.Entries) > 0 {
			var timingParts []string
			for _, t := range timing.Entries {
				if t.Duration != "" {
					timingParts = append(timingParts, fmt.Sprintf("%s (%s)", t.Phase, t.Duration))
				}
			}
			if len(timingParts) > 0 {
				lines = append(lines, fmt.Sprintf("- Phase timing: %s", strings.Join(timingParts, ", ")))
			}
		}

		loops, err := state.LoadLoopCounts(artifactsDir)
		if err == nil && len(loops) > 0 {
			var loopParts []string
			for k, v := range loops {
				loopParts = append(loopParts, fmt.Sprintf("%s=%d", k, v))
			}
			lines = append(lines, fmt.Sprintf("- Loop iterations: %s", strings.Join(loopParts, ", ")))
		}

		runStatus := gatherRunStatus(auditDir, artifactsDir)
		if runStatus != "" {
			lines = append(lines, runStatus)
		}

		phaseLogs := gatherPhaseLogs(auditDir, artifactsDir)
		feedback := gatherFeedback(auditDir, artifactsDir)

		if len(lines) > 0 || phaseLogs != "" || feedback != "" {
			section := fmt.Sprintf("### Ticket: %s\n%s", ticket, strings.Join(lines, "\n"))
			if phaseLogs != "" {
				section += "\n\n" + phaseLogs
			}
			if feedback != "" {
				section += "\n\n" + feedback
			}
			parts = append(parts, section)
		}
	}

	if len(parts) == 0 {
		return ""
	}
	return "The following data is from previous workflow runs. Use it to inform your suggestions.\n\n" +
		strings.Join(parts, "\n\n")
}

// isTicketAuditDir checks if a directory looks like a ticket audit dir
// (contains timing.json, costs.json, or state.json).
func isTicketAuditDir(dir string) bool {
	for _, f := range []string{"timing.json", "costs.json", "state.json"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			return true
		}
	}
	return false
}

func gatherPhaseLogs(auditDir, artifactsDir string) string {
	// Archived iteration logs from previous loop iterations
	auditMatches, _ := filepath.Glob(filepath.Join(auditDir, "logs", "phase-*.iter-*.log"))
	// Final iteration logs in current artifacts dir
	artifactMatches, _ := filepath.Glob(filepath.Join(artifactsDir, "logs", "phase-*.log"))

	all := make([]string, 0, len(auditMatches)+len(artifactMatches))
	all = append(all, auditMatches...)
	all = append(all, artifactMatches...)
	sort.Strings(all)

	var entries []string
	for _, path := range all {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := strings.TrimSpace(string(data))
		if content == "" {
			continue
		}
		entries = append(entries, fmt.Sprintf("#### Log: %s\n```\n%s\n```", filepath.Base(path), content))
	}
	return strings.Join(entries, "\n\n")
}

func gatherFeedback(auditDir, artifactsDir string) string {
	auditMatches, _ := filepath.Glob(filepath.Join(auditDir, "feedback", "*.md"))
	artifactMatches, _ := filepath.Glob(filepath.Join(artifactsDir, "feedback", "*.md"))

	all := make([]string, 0, len(auditMatches)+len(artifactMatches))
	all = append(all, auditMatches...)
	all = append(all, artifactMatches...)
	sort.Strings(all)

	var entries []string
	for _, path := range all {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := strings.TrimSpace(string(data))
		if content == "" {
			continue
		}
		entries = append(entries, fmt.Sprintf("#### Feedback: %s\n%s", filepath.Base(path), content))
	}
	return strings.Join(entries, "\n\n")
}

func gatherRunStatus(auditDir, artifactsDir string) string {
	data, err := os.ReadFile(filepath.Join(auditDir, "state.json"))
	if err != nil {
		data, err = os.ReadFile(filepath.Join(artifactsDir, "state.json"))
		if err != nil {
			return ""
		}
	}
	var s struct {
		PhaseIndex int    `json:"phase_index"`
		Status     string `json:"status"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return ""
	}
	if s.Status == "" {
		return ""
	}
	if s.Status == "failed" {
		return fmt.Sprintf("- Run status: failed (at phase %d)", s.PhaseIndex+1)
	}
	return fmt.Sprintf("- Run status: %s", s.Status)
}

// writeContextFile writes content to .orc/artifacts/<name> and returns the absolute path.
func writeContextFile(projectRoot, name, content string) (string, error) {
	dir := filepath.Join(projectRoot, ".orc", "artifacts")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("creating artifacts dir: %w", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("writing context file: %w", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path, nil
	}
	return abs, nil
}

// OneShot reads the current config, sends it to Claude with the user's instruction,
// validates the output, and writes changed files.
func OneShot(ctx context.Context, projectRoot, instruction string) error {
	configYAML, phaseFiles, err := readOrcFiles(projectRoot)
	if err != nil {
		return err
	}
	auditSummary := readAuditSummary(projectRoot)
	prompt := buildOneShotPrompt(configYAML, phaseFiles, auditSummary, instruction)

	promptFile, err := writeContextFile(projectRoot, ".improve-prompt.md", prompt)
	if err != nil {
		return err
	}
	defer os.Remove(promptFile)

	fmt.Printf("  %sReading current config...%s\n\n", ux.Dim, ux.Reset)

	output, err := runClaudeCapture(ctx, promptFile)
	if err != nil {
		return err
	}

	if output == "" {
		return fmt.Errorf("claude returned no output")
	}

	blocks := fileblocks.Parse(output)
	if len(blocks) == 0 {
		return fmt.Errorf("no file blocks found in output — expected fenced code blocks with file= annotations")
	}

	return writeChanges(projectRoot, blocks)
}

func runClaudeCapture(ctx context.Context, promptFile string) (string, error) {
	cmd := exec.CommandContext(ctx, "claude", "-p", "Read the file at "+promptFile+" and follow its instructions exactly.",
		"--model", "opus",
		"--effort", "high",
		"--output-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
	)
	cmd.Env = dispatch.FilteredEnv()
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("starting claude: %w", err)
	}

	result, streamErr := dispatch.ProcessStream(ctx, stdout, os.Stdout, nil, nil)

	if err := cmd.Wait(); err != nil && streamErr == nil {
		return "", fmt.Errorf("claude: %w", err)
	}
	if streamErr != nil {
		return "", streamErr
	}

	return result.Text, nil
}

func writeChanges(projectRoot string, blocks []fileblocks.FileBlock) error {
	absRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return fmt.Errorf("resolving project root: %w", err)
	}

	var kept []fileblocks.FileBlock
	for _, b := range blocks {
		if !strings.HasPrefix(b.Path, ".orc/") {
			continue
		}
		fullPath := filepath.Join(absRoot, b.Path)
		if !isWithinDir(absRoot, fullPath) {
			continue
		}
		kept = append(kept, b)
	}
	if len(kept) == 0 {
		return fmt.Errorf("no changes produced (all file blocks were outside .orc/)")
	}

	hasConfig := false
	for _, b := range kept {
		if b.Path == ".orc/config.yaml" {
			hasConfig = true
			break
		}
	}

	if hasConfig {
		tmpDir, err := os.MkdirTemp("", "orc-improve-*")
		if err != nil {
			return fmt.Errorf("creating temp dir: %w", err)
		}
		defer os.RemoveAll(tmpDir)

		if err := copyOrcDir(projectRoot, tmpDir); err != nil {
			return fmt.Errorf("copying .orc/ for validation: %w", err)
		}

		for _, b := range kept {
			fullPath := filepath.Join(tmpDir, b.Path)
			if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
				return fmt.Errorf("creating directory for %s: %w", b.Path, err)
			}
			if err := os.WriteFile(fullPath, []byte(b.Content+"\n"), 0644); err != nil {
				return fmt.Errorf("writing %s to temp dir: %w", b.Path, err)
			}
		}

		if _, err := config.Load(filepath.Join(tmpDir, ".orc", "config.yaml"), tmpDir); err != nil {
			return fmt.Errorf("invalid config: %w", err)
		}
	}

	for _, b := range kept {
		fullPath := filepath.Join(absRoot, b.Path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return fmt.Errorf("creating directory for %s: %w", b.Path, err)
		}
		if err := os.WriteFile(fullPath, []byte(b.Content+"\n"), 0644); err != nil {
			return fmt.Errorf("writing %s: %w", b.Path, err)
		}
	}

	fmt.Println()
	for _, b := range kept {
		fmt.Printf("  %s✓ Updated %s%s\n", ux.Green, b.Path, ux.Reset)
	}
	return nil
}

// isWithinDir checks that path is within the base directory.
func isWithinDir(base, path string) bool {
	rel, err := filepath.Rel(base, path)
	return err == nil && !strings.HasPrefix(rel, "..") && rel != ".."
}

func copyOrcDir(projectRoot, tmpDir string) error {
	srcOrc := filepath.Join(projectRoot, ".orc")
	return filepath.WalkDir(srcOrc, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(srcOrc, path)
		if rel == "artifacts" || strings.HasPrefix(rel, "artifacts"+string(filepath.Separator)) ||
			rel == "audit" || strings.HasPrefix(rel, "audit"+string(filepath.Separator)) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		dstPath := filepath.Join(tmpDir, ".orc", rel)
		if d.IsDir() {
			return os.MkdirAll(dstPath, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
			return err
		}
		return os.WriteFile(dstPath, data, 0644)
	})
}

// Interactive launches Claude in interactive mode with workflow context pre-loaded.
func Interactive(projectRoot string) error {
	configYAML, phaseFiles, err := readOrcFiles(projectRoot)
	if err != nil {
		return err
	}
	auditSummary := readAuditSummary(projectRoot)
	ctx := buildInteractiveContext(configYAML, phaseFiles, auditSummary)

	absRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return fmt.Errorf("resolving project root: %w", err)
	}
	ctxFile, err := writeContextFile(absRoot, ".improve-context.md", ctx)
	if err != nil {
		return err
	}
	// No defer Remove — syscall.Exec replaces the process. File lives in artifacts (gitignored).

	claudePath, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude not found: %w", err)
	}

	fmt.Printf("  %sLoading workflow context...%s\n", ux.Dim, ux.Reset)

	sysPrompt := "Your full workflow context is at " + ctxFile + ". Read it immediately before saying anything."
	args := []string{"claude", "--model", "opus", "--effort", "high", "--append-system-prompt", sysPrompt, "Analyze my workflow and suggest improvements."}
	env := dispatch.FilteredEnv()
	return syscall.Exec(claudePath, args, env)
}
