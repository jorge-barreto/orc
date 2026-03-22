package scaffold

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/contextgather"
	"github.com/jorge-barreto/orc/internal/dispatch"
	"github.com/jorge-barreto/orc/internal/fileblocks"
	"github.com/jorge-barreto/orc/internal/ux"
)

// Init creates a new .orc/ directory with AI-generated workflow config and prompt files.
func Init(ctx context.Context, targetDir, userPrompt string) error {
	orcDir := filepath.Join(targetDir, ".orc")
	if _, err := os.Stat(orcDir); err == nil {
		return fmt.Errorf(".orc directory already exists in %s", targetDir)
	}

	return initWithAI(ctx, targetDir, userPrompt)
}

// InitRecipe scaffolds a .orc/ directory from a named built-in recipe.
func InitRecipe(targetDir, recipeName string) error {
	orcDir := filepath.Join(targetDir, ".orc")
	if _, err := os.Stat(orcDir); err == nil {
		return fmt.Errorf(".orc directory already exists in %s", targetDir)
	}

	recipe, err := GetRecipe(recipeName)
	if err != nil {
		return err
	}

	var written []string
	for relPath, content := range recipe.Files {
		fullPath := filepath.Join(targetDir, relPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return fmt.Errorf("creating directory for %s: %w", relPath, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("writing %s: %w", relPath, err)
		}
		written = append(written, relPath)
	}

	gitignorePath := filepath.Join(targetDir, ".orc", ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte("artifacts/\n"), 0644); err != nil {
		return fmt.Errorf("writing .orc/.gitignore: %w", err)
	}
	written = append(written, ".orc/.gitignore")

	// Sanity check: embedded config must be valid
	configPath := filepath.Join(targetDir, ".orc", "config.yaml")
	if _, err := config.Load(configPath, targetDir); err != nil {
		return fmt.Errorf("recipe %q produced invalid config: %w", recipeName, err)
	}

	printSuccess("recipe: "+recipeName, written)

	if cfg, err := config.Load(configPath, targetDir); err == nil {
		fmt.Printf("\n  Workflow: %s%s%s\n", ux.Bold, renderWorkflowSummary(cfg.Phases), ux.Reset)
	}

	fmt.Printf("\n  Next: %sorc run <ticket> --dry-run%s\n\n", ux.Cyan, ux.Reset)
	return nil
}

// InitWorkflow adds a named workflow to an existing .orc/ project.
// If recipe is non-empty, uses that recipe's config; otherwise creates a
// minimal starter workflow.
func InitWorkflow(targetDir, name, recipe string) error {
	if name != filepath.Base(name) || name == ".." || name == "." {
		return fmt.Errorf("invalid workflow name %q: must not contain path separators", name)
	}
	orcDir := filepath.Join(targetDir, ".orc")
	if _, err := os.Stat(orcDir); os.IsNotExist(err) {
		return fmt.Errorf("no .orc/ directory found — run 'orc init' first")
	}

	workflowPath := filepath.Join(orcDir, "workflows", name+".yaml")
	if _, err := os.Stat(workflowPath); err == nil {
		return fmt.Errorf("workflow %q already exists at %s", name, workflowPath)
	}

	if err := os.MkdirAll(filepath.Join(orcDir, "workflows"), 0755); err != nil {
		return fmt.Errorf("creating workflows dir: %w", err)
	}

	var content string
	var promptFiles map[string]string
	if recipe != "" {
		r, err := getRecipeFn(recipe)
		if err != nil {
			return err
		}
		c, ok := r.Files[".orc/config.yaml"]
		if !ok {
			return fmt.Errorf("recipe %q has no config.yaml", recipe)
		}
		content = c
		promptFiles = make(map[string]string)
		for path, body := range r.Files {
			if path != ".orc/config.yaml" {
				promptFiles[path] = body
			}
		}
	} else {
		content = fmt.Sprintf(`name: %s
phases:
  - name: plan
    type: agent
    prompt: .orc/phases/%s-plan.md
    outputs:
      - plan.md

  - name: implement
    type: agent
    prompt: .orc/phases/%s-implement.md
    outputs:
      - summary.md
`, name, name, name)
	}

	if err := os.WriteFile(workflowPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing workflow: %w", err)
	}

	// Write recipe prompt files (skip files that already exist)
	var created []string
	for relPath, body := range promptFiles {
		fullPath := filepath.Join(targetDir, relPath)
		if _, err := os.Stat(fullPath); err == nil {
			continue // don't overwrite existing files
		}
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return fmt.Errorf("creating directory for %s: %w", relPath, err)
		}
		if err := os.WriteFile(fullPath, []byte(body), 0644); err != nil {
			return fmt.Errorf("writing %s: %w", relPath, err)
		}
		created = append(created, relPath)
	}

	fmt.Printf("%s✓ Created workflow %q at %s%s\n", ux.Green, name, workflowPath, ux.Reset)
	if len(created) > 0 {
		for _, p := range created {
			fmt.Printf("  %s+ %s%s\n", ux.Dim, p, ux.Reset)
		}
	}
	fmt.Printf("  Next: edit %s, then %sorc run %s <ticket>%s\n\n", workflowPath, ux.Cyan, name, ux.Reset)
	return nil
}

// ListRecipes prints all available built-in recipes with descriptions.
func ListRecipes() {
	recipes := AllRecipes()
	fmt.Printf("\nAvailable recipes:\n\n")
	for _, r := range recipes {
		fmt.Printf("  %s%-20s%s %s\n", ux.Cyan, r.Name, ux.Reset, r.Description)
		fmt.Printf("  %s%-20s%s %s\n", ux.Dim, "", ux.Reset, r.Workflow)
		fmt.Println()
	}
	fmt.Printf("  Usage: %sorc init --recipe <name>%s\n\n", ux.Bold, ux.Reset)
}

// initWithAI gathers project context, calls claude with retries, and writes AI-generated files.
// Falls back to a default template if all attempts fail.
func initWithAI(ctx context.Context, targetDir, userPrompt string) error {
	fmt.Printf("\n  %sAnalyzing project...%s\n", ux.Dim, ux.Reset)

	pc, err := contextgather.Gather(targetDir)
	if err != nil {
		return fmt.Errorf("gathering context: %w", err)
	}

	prompt := buildInitPrompt(pc.Render(), userPrompt)

	const maxAttempts = 3
	var blocks []fileblocks.FileBlock
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt == 1 {
			fmt.Printf("  %sGenerating workflow config...%s\n", ux.Dim, ux.Reset)
		} else {
			fmt.Printf("  %s↺ Retrying (%d/%d): %v%s\n", ux.Yellow, attempt, maxAttempts, lastErr, ux.Reset)
		}

		currentPrompt := prompt
		if attempt > 1 {
			currentPrompt = prompt + fmt.Sprintf(retryFeedback, lastErr)
		}

		blocks, lastErr = generateConfig(ctx, currentPrompt)
		if lastErr == nil {
			break
		}
	}

	if lastErr != nil {
		fmt.Printf("\n  %s⚠ AI generation failed after %d attempts: %v%s\n",
			ux.Yellow, maxAttempts, lastErr, ux.Reset)
		fmt.Printf("  %sUsing default workflow template...%s\n", ux.Dim, ux.Reset)
		return writeFallbackConfig(targetDir)
	}

	// Write validated files to the target directory
	written, err := writeBlocks(targetDir, blocks)
	if err != nil {
		return fmt.Errorf("writing generated files: %w", err)
	}

	// Write .gitignore (deterministic, not AI-generated)
	gitignorePath := filepath.Join(targetDir, ".orc", ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte("artifacts/\n"), 0644); err != nil {
		return fmt.Errorf("writing .orc/.gitignore: %w", err)
	}
	written = append(written, ".orc/.gitignore")

	printSuccess("AI-generated", written)

	// Config is already validated by generateConfig; load for workflow summary
	configPath := filepath.Join(targetDir, ".orc", "config.yaml")
	if cfg, err := config.Load(configPath, targetDir); err == nil {
		fmt.Printf("\n  Workflow: %s%s%s\n", ux.Bold, renderWorkflowSummary(cfg.Phases), ux.Reset)
	}

	fmt.Printf("\n  Next: %sorc run <ticket> --dry-run%s\n\n", ux.Cyan, ux.Reset)
	return nil
}

// generateConfig calls claude, parses the output, and validates the generated config
// in a temp directory. Returns the validated file blocks or an error.
func generateConfig(ctx context.Context, prompt string) ([]fileblocks.FileBlock, error) {
	blocks, err := runClaude(ctx, prompt)
	if err != nil {
		return nil, err
	}

	hasConfig := false
	for _, b := range blocks {
		if b.Path == ".orc/config.yaml" {
			hasConfig = true
		}
	}
	if !hasConfig {
		return nil, fmt.Errorf("output missing .orc/config.yaml")
	}

	// Write to temp dir and validate
	tmpDir, err := os.MkdirTemp("", "orc-init-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	for _, b := range blocks {
		if !strings.HasPrefix(b.Path, ".orc/") {
			continue
		}
		fullPath := filepath.Join(tmpDir, b.Path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return nil, fmt.Errorf("creating temp dir for %s: %w", b.Path, err)
		}
		if err := os.WriteFile(fullPath, []byte(b.Content), 0644); err != nil {
			return nil, fmt.Errorf("writing temp %s: %w", b.Path, err)
		}
	}

	if _, err := config.Load(filepath.Join(tmpDir, ".orc", "config.yaml"), tmpDir); err != nil {
		return nil, fmt.Errorf("generated config is invalid: %w", err)
	}

	return blocks, nil
}

// writeBlocks writes validated file blocks to the target directory.
func writeBlocks(targetDir string, blocks []fileblocks.FileBlock) ([]string, error) {
	var written []string
	for _, b := range blocks {
		if !strings.HasPrefix(b.Path, ".orc/") {
			continue
		}
		fullPath := filepath.Join(targetDir, b.Path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return written, fmt.Errorf("creating directory for %s: %w", b.Path, err)
		}
		if err := os.WriteFile(fullPath, []byte(b.Content), 0644); err != nil {
			return written, fmt.Errorf("writing %s: %w", b.Path, err)
		}
		written = append(written, b.Path)
	}
	return written, nil
}

// printSuccess prints the initialization success message and file list.
func printSuccess(source string, written []string) {
	fmt.Printf("\n%s%s  ✓ Initialized .orc/ directory (%s)%s\n\n", ux.Bold, ux.Green, source, ux.Reset)
	fmt.Printf("  Created:\n")
	for _, path := range written {
		fmt.Printf("    %s%s%s\n", ux.Cyan, path, ux.Reset)
	}
}

// getRecipeFn is the function used to fetch recipes. Tests can override this.
var getRecipeFn = GetRecipe

// runClaude is the function used to invoke claude. Tests can override this.
var runClaude = runClaudeCaptureDefault

// runClaudeCaptureDefault writes the prompt to a temp file and invokes claude -p
// with a short instruction to read it. This avoids ARG_MAX limits when the prompt
// (schema + examples + project context) exceeds the OS command-line size limit.
func runClaudeCaptureDefault(ctx context.Context, prompt string) ([]fileblocks.FileBlock, error) {
	f, err := os.CreateTemp("", "orc-init-prompt-*.md")
	if err != nil {
		return nil, fmt.Errorf("creating prompt file: %w", err)
	}
	promptFile := f.Name()
	defer os.Remove(promptFile)

	if _, err := f.WriteString(prompt); err != nil {
		f.Close()
		return nil, fmt.Errorf("writing prompt file: %w", err)
	}
	f.Close()

	schema := `{"type":"object","properties":{"files":{"type":"array","items":{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}}},"required":["files"]}`

	cmd := exec.CommandContext(ctx, "claude", "-p",
		"Read the file at "+promptFile+" and follow its instructions exactly.",
		"--model", "opus", "--effort", "high",
		"--output-format", "json", "--json-schema", schema)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	cmd.Env = dispatch.FilteredEnv()
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("claude: %w", err)
	}

	var resp struct {
		StructuredOutput struct {
			Files []fileblocks.FileBlock `json:"files"`
		} `json:"structured_output"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("parsing claude output: %w", err)
	}
	if len(resp.StructuredOutput.Files) == 0 {
		return nil, fmt.Errorf("no files in structured output")
	}
	return resp.StructuredOutput.Files, nil
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
