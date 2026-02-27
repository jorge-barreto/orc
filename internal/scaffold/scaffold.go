package scaffold

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/contextgather"
	"github.com/jorge-barreto/orc/internal/fileblocks"
	"github.com/jorge-barreto/orc/internal/ux"
)

// Init creates a new .orc/ directory with AI-generated workflow config and prompt files.
func Init(ctx context.Context, targetDir string) error {
	orcDir := filepath.Join(targetDir, ".orc")
	if _, err := os.Stat(orcDir); err == nil {
		return fmt.Errorf(".orc directory already exists in %s", targetDir)
	}

	return initWithAI(ctx, targetDir)
}

// initWithAI gathers project context, calls claude, and writes AI-generated files.
func initWithAI(ctx context.Context, targetDir string) error {
	fmt.Printf("\n  %sAnalyzing project...%s\n", ux.Dim, ux.Reset)

	pc, err := contextgather.Gather(targetDir)
	if err != nil {
		return fmt.Errorf("gathering context: %w", err)
	}

	prompt := buildInitPrompt(pc.Render())

	fmt.Printf("  %sGenerating workflow config...%s\n", ux.Dim, ux.Reset)

	output, err := runClaudeCapture(ctx, prompt)
	if err != nil {
		return err
	}

	blocks := fileblocks.Parse(output)
	if len(blocks) == 0 {
		return fmt.Errorf("no file blocks in claude output")
	}

	// Validate that we got a config.yaml
	hasConfig := false
	for _, b := range blocks {
		if b.Path == ".orc/config.yaml" {
			hasConfig = true
		}
	}
	if !hasConfig {
		return fmt.Errorf("claude output missing .orc/config.yaml")
	}

	// Create directories and write files
	var written []string
	for _, b := range blocks {
		if !strings.HasPrefix(b.Path, ".orc/") {
			continue // security: only write inside .orc/
		}

		fullPath := filepath.Join(targetDir, b.Path)
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("creating directory for %s: %w", b.Path, err)
		}
		if err := os.WriteFile(fullPath, []byte(b.Content), 0644); err != nil {
			return fmt.Errorf("writing %s: %w", b.Path, err)
		}
		written = append(written, b.Path)
	}

	// Print success
	fmt.Printf("\n%s%s  ✓ Initialized .orc/ directory (AI-generated)%s\n\n", ux.Bold, ux.Green, ux.Reset)
	fmt.Printf("  Created:\n")
	for _, path := range written {
		fmt.Printf("    %s%s%s\n", ux.Cyan, path, ux.Reset)
	}

	// Try to load and validate the generated config; print workflow summary if valid
	configPath := filepath.Join(targetDir, ".orc", "config.yaml")
	cfg, err := config.Load(configPath, targetDir)
	if err != nil {
		fmt.Printf("\n  %s⚠ Generated config has validation issues: %v%s\n", ux.Yellow, err, ux.Reset)
		fmt.Printf("  %sPlease review and fix .orc/config.yaml%s\n", ux.Dim, ux.Reset)
	} else {
		summary := renderWorkflowSummary(cfg.Phases)
		fmt.Printf("\n  Workflow: %s%s%s\n", ux.Bold, summary, ux.Reset)
	}

	fmt.Printf("\n  Next: %sorc run <ticket> --dry-run%s\n\n", ux.Cyan, ux.Reset)
	return nil
}

// runClaudeCapture invokes claude -p with the given prompt and returns stdout.
func runClaudeCapture(ctx context.Context, prompt string) (string, error) {
	cmd := exec.CommandContext(ctx, "claude", "-p", prompt, "--model", "opus")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	cmd.Env = filteredEnv()
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("claude: %w", err)
	}
	return stdout.String(), nil
}

// filteredEnv returns the current environment with CLAUDECODE stripped.
func filteredEnv() []string {
	var env []string
	for _, e := range os.Environ() {
		key := strings.SplitN(e, "=", 2)[0]
		if strings.HasPrefix(key, "CLAUDECODE") {
			continue
		}
		env = append(env, e)
	}
	return env
}

// renderWorkflowSummary builds a human-readable workflow line.
// Sequential phases are joined with →, parallel phases with ∥.
func renderWorkflowSummary(phases []config.Phase) string {
	// Build map of parallel partners: the "earlier" phase -> "earlier ∥ later"
	parallelOf := make(map[string]string)
	skipSelf := make(map[string]bool)
	for _, p := range phases {
		if p.ParallelWith != "" {
			parallelOf[p.ParallelWith] = fmt.Sprintf("%s ∥ %s", p.ParallelWith, p.Name)
			skipSelf[p.Name] = true
		}
	}

	var parts []string
	for _, p := range phases {
		if skipSelf[p.Name] {
			continue
		}
		if group, ok := parallelOf[p.Name]; ok {
			parts = append(parts, group)
		} else {
			parts = append(parts, p.Name)
		}
	}
	return strings.Join(parts, " → ")
}
