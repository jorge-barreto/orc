package eval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/jorge-barreto/orc/internal/config"
)

type Fixture struct {
	Ref         string            `yaml:"ref"`
	Ticket      string            `yaml:"ticket"`
	Vars        map[string]string `yaml:"vars"`
	Description string            `yaml:"description"`
}

type Criterion struct {
	Name   string  `yaml:"name"`
	Check  string  `yaml:"check"`
	Judge  bool    `yaml:"judge"`
	Prompt string  `yaml:"prompt"`
	Expect string  `yaml:"expect"`
	Weight float64 `yaml:"weight"`
}

type Rubric struct {
	Criteria []Criterion `yaml:"criteria"`
}

func DiscoverCases(projectRoot string) ([]string, error) {
	evalsDir := filepath.Join(projectRoot, ".orc", "evals")
	entries, err := os.ReadDir(evalsDir)
	if err != nil {
		return nil, fmt.Errorf("eval: .orc/evals/ not found: %w", err)
	}
	var cases []string
	for _, e := range entries {
		if e.IsDir() {
			cases = append(cases, e.Name())
		}
	}
	sort.Strings(cases)
	return cases, nil
}

var refRegex = regexp.MustCompile(`^[A-Za-z0-9._/~^{}-]+$`)

func LoadFixture(caseDir string) (*Fixture, error) {
	data, err := os.ReadFile(filepath.Join(caseDir, "fixture.yaml"))
	if err != nil {
		return nil, fmt.Errorf("eval: reading fixture.yaml: %w", err)
	}
	var f Fixture
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("eval: parsing fixture.yaml: %w", err)
	}
	if f.Ref == "" {
		return nil, fmt.Errorf("eval: fixture.yaml: ref is required")
	}
	if f.Ticket == "" {
		return nil, fmt.Errorf("eval: fixture.yaml: ticket is required")
	}
	// Path traversal check: ticket must be a simple filename, not a path
	if f.Ticket != filepath.Base(f.Ticket) {
		return nil, fmt.Errorf("eval: fixture.yaml: ticket %q must not contain path separators", f.Ticket)
	}
	if !refRegex.MatchString(f.Ref) {
		return nil, fmt.Errorf("eval: fixture.yaml: ref %q contains invalid characters", f.Ref)
	}
	return &f, nil
}

func LoadRubric(caseDir, projectRoot string) (*Rubric, error) {
	data, err := os.ReadFile(filepath.Join(caseDir, "rubric.yaml"))
	if err != nil {
		return nil, fmt.Errorf("eval: reading rubric.yaml: %w", err)
	}
	var r Rubric
	if err := yaml.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("eval: parsing rubric.yaml: %w", err)
	}
	for i, c := range r.Criteria {
		if c.Name == "" {
			return nil, fmt.Errorf("eval: rubric.yaml: criterion %d: name is required", i+1)
		}
		if c.Check == "" && !c.Judge {
			return nil, fmt.Errorf("eval: rubric.yaml: criterion %q: must have check or judge: true", c.Name)
		}
		if c.Check != "" && c.Judge {
			return nil, fmt.Errorf("eval: rubric.yaml: criterion %q: cannot have both check and judge: true", c.Name)
		}
		if c.Weight <= 0 {
			return nil, fmt.Errorf("eval: rubric.yaml: criterion %q: weight must be > 0", c.Name)
		}
		if c.Judge && c.Prompt == "" {
			return nil, fmt.Errorf("eval: rubric.yaml: criterion %q: judge criteria require a prompt file", c.Name)
		}
		if c.Judge && c.Prompt != "" {
			// Path traversal check
			absPath, err := filepath.Abs(filepath.Join(projectRoot, c.Prompt))
			if err != nil {
				return nil, fmt.Errorf("eval: rubric.yaml: criterion %q: resolving prompt path: %w", c.Name, err)
			}
			if !strings.HasPrefix(absPath, projectRoot+string(filepath.Separator)) {
				return nil, fmt.Errorf("eval: rubric.yaml: criterion %q: prompt path escapes project root", c.Name)
			}
		}
	}
	return &r, nil
}

func ConfigFingerprint(configPath string, cfg *config.Config, projectRoot string) (string, error) {
	h := sha256.New()

	// Collect file paths for deterministic hashing
	var filePaths []string
	filePaths = append(filePaths, configPath)
	for _, phase := range cfg.Phases {
		if phase.Prompt != "" {
			filePaths = append(filePaths, filepath.Join(projectRoot, phase.Prompt))
		}
	}
	sort.Strings(filePaths)

	// Hash files
	for _, path := range filePaths {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("eval: fingerprint: reading %s: %w", path, err)
		}
		fmt.Fprintf(h, "%s:", path)
		h.Write(data)
	}

	// Hash inline Run strings (sorted for determinism)
	var runStrings []string
	for _, phase := range cfg.Phases {
		if phase.Run != "" {
			runStrings = append(runStrings, phase.Run)
		}
	}
	sort.Strings(runStrings)
	for _, run := range runStrings {
		io.WriteString(h, run)
	}

	return hex.EncodeToString(h.Sum(nil))[:8], nil
}

func CreateWorktree(ctx context.Context, projectRoot, ref, caseName string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "orc-eval-"+caseName+"-")
	if err != nil {
		return "", fmt.Errorf("eval: creating temp dir: %w", err)
	}
	// Remove the temp dir — git worktree add creates the directory itself
	os.RemoveAll(tmpDir)

	cmd := exec.CommandContext(ctx, "git", "worktree", "add", tmpDir, ref)
	cmd.Dir = projectRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("eval: git worktree add: %w\n%s", err, out)
	}
	return tmpDir, nil
}

func RemoveWorktree(projectRoot, worktreePath string) error {
	cmd := exec.Command("git", "worktree", "remove", "--force", worktreePath)
	cmd.Dir = projectRoot
	cmd.Run() // ignore error — fallback below
	return os.RemoveAll(worktreePath)
}

func copyOrcDir(projectRoot, worktreePath, configPath string, cfg *config.Config) error {
	if strings.Contains(configPath, filepath.Join(".orc", "workflows")+string(filepath.Separator)) ||
		strings.HasSuffix(configPath, filepath.Join(".orc", "workflows", filepath.Base(configPath))) {
		// Multi-workflow layout
		destDir := filepath.Join(worktreePath, ".orc", "workflows")
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			return err
		}
		if err := copyFile(configPath, filepath.Join(destDir, filepath.Base(configPath))); err != nil {
			return err
		}
		// Copy root config.yaml if it exists
		rootConfig := filepath.Join(projectRoot, ".orc", "config.yaml")
		if _, err := os.Stat(rootConfig); err == nil {
			if err := copyFile(rootConfig, filepath.Join(worktreePath, ".orc", "config.yaml")); err != nil {
				return err
			}
		}
	} else {
		// Single-config layout
		destDir := filepath.Join(worktreePath, ".orc")
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			return err
		}
		if err := copyFile(configPath, filepath.Join(destDir, "config.yaml")); err != nil {
			return err
		}
	}

	// Copy all referenced prompt files
	for _, phase := range cfg.Phases {
		if phase.Prompt == "" {
			continue
		}
		src := filepath.Join(projectRoot, phase.Prompt)
		dst := filepath.Join(worktreePath, phase.Prompt)
		if _, err := os.Stat(src); os.IsNotExist(err) {
			return fmt.Errorf("eval: copyOrcDir: prompt file missing: %s", src)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := copyFile(src, dst); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("eval: copyFile: reading %s: %w", src, err)
	}
	return os.WriteFile(dst, data, 0o644)
}
