package eval

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/state"
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
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("eval: parsing fixture.yaml: %w", err)
	}
	if f.Ref == "" {
		return nil, fmt.Errorf("eval: fixture.yaml: ref is required")
	}
	if f.Ticket == "" {
		return nil, fmt.Errorf("eval: fixture.yaml: ticket is required")
	}
	// Path traversal check: ticket must be a simple filename, not a path
	if f.Ticket == ".." || f.Ticket == "." {
		return nil, fmt.Errorf("eval: fixture.yaml: ticket %q is not allowed", f.Ticket)
	}
	if f.Ticket != filepath.Base(f.Ticket) {
		return nil, fmt.Errorf("eval: fixture.yaml: ticket %q must not contain path separators", f.Ticket)
	}
	if !refRegex.MatchString(f.Ref) {
		return nil, fmt.Errorf("eval: fixture.yaml: ref %q contains invalid characters", f.Ref)
	}
	builtins := map[string]bool{
		"TICKET": true, "ARTIFACTS_DIR": true,
		"WORK_DIR": true, "PROJECT_ROOT": true,
		"PHASE_INDEX": true, "PHASE_COUNT": true,
		"WORKFLOW": true,
	}
	for k := range f.Vars {
		if builtins[k] {
			return nil, fmt.Errorf("eval: fixture.yaml: var %q overrides a built-in variable", k)
		}
	}
	return &f, nil
}

func LoadRubric(caseDir, projectRoot string) (*Rubric, error) {
	data, err := os.ReadFile(filepath.Join(caseDir, "rubric.yaml"))
	if err != nil {
		return nil, fmt.Errorf("eval: reading rubric.yaml: %w", err)
	}
	var r Rubric
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&r); err != nil {
		return nil, fmt.Errorf("eval: parsing rubric.yaml: %w", err)
	}
	seen := make(map[string]bool)
	for i, c := range r.Criteria {
		if c.Name == "" {
			return nil, fmt.Errorf("eval: rubric.yaml: criterion %d: name is required", i+1)
		}
		if seen[c.Name] {
			return nil, fmt.Errorf("eval: rubric.yaml: duplicate criterion name %q", c.Name)
		}
		seen[c.Name] = true
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
			if _, err := os.Stat(absPath); err != nil {
				return nil, fmt.Errorf("eval: rubric.yaml: criterion %q: prompt file %q does not exist", c.Name, c.Prompt)
			}
		}
		if c.Expect != "" {
			if c.Judge {
				if !isValidJudgeExpect(c.Expect) {
					return nil, fmt.Errorf("eval: rubric.yaml: criterion %q: invalid expect %q (use \">= N\", \"> N\", \"<= N\", \"< N\", or \"== N\")", c.Name, c.Expect)
				}
			} else if c.Check != "" {
				if !isValidScriptExpect(c.Expect) {
					return nil, fmt.Errorf("eval: rubric.yaml: criterion %q: invalid expect %q (use \"exit N\")", c.Name, c.Expect)
				}
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
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = 5 * time.Second
	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(tmpDir)
		pruneCtx, pruneCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer pruneCancel()
		pruneCmd := exec.CommandContext(pruneCtx, "git", "worktree", "prune")
		pruneCmd.Dir = projectRoot
		pruneCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		pruneCmd.Cancel = func() error { return syscall.Kill(-pruneCmd.Process.Pid, syscall.SIGTERM) }
		pruneCmd.WaitDelay = 5 * time.Second
		pruneCmd.Run() //nolint:errcheck
		return "", fmt.Errorf("eval: git worktree add: %w\n%s", err, out)
	}
	return tmpDir, nil
}

func RemoveWorktree(projectRoot, worktreePath string) error {
	removeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(removeCtx, "git", "worktree", "remove", "--force", worktreePath)
	cmd.Dir = projectRoot
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = 5 * time.Second
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

// RunResult holds the outcome of running orc in a worktree.
type RunResult struct {
	ArtifactsDir    string
	CostUSD         float64
	DurationSeconds float64
	Status          string
	Err             error
}

// HistoryEntry holds one eval run's summary.
type HistoryEntry struct {
	Timestamp         string                      `json:"timestamp"`
	ConfigFingerprint string                      `json:"config_fingerprint"`
	Cases             map[string]CaseHistoryEntry `json:"cases"`
}

// CaseHistoryEntry holds the persisted result for one case.
type CaseHistoryEntry struct {
	Score           int                `json:"score"`
	CostUSD         float64            `json:"cost_usd"`
	DurationSeconds float64            `json:"duration_seconds"`
	Details         map[string]float64 `json:"details"`
}

// History holds all historical eval runs.
type History struct {
	Runs []HistoryEntry `json:"runs"`
}

// RunWorkflow executes orc in the given worktree and returns the result.
// It never returns a non-nil error — partial results are always returned
// so the rubric evaluator can still run.
func RunWorkflow(ctx context.Context, worktreePath, ticket, workflowName string, vars map[string]string) (*RunResult, error) {
	orcBinary, err := os.Executable()
	if err != nil {
		return &RunResult{Status: "failed", Err: fmt.Errorf("eval: os.Executable: %w", err)}, nil
	}
	args := []string{"run", ticket, "--auto"}
	if workflowName != "" {
		args = append(args, "-w", workflowName)
	}
	cmd := exec.CommandContext(ctx, orcBinary, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = 5 * time.Second
	cmd.Dir = worktreePath

	// Build env: inherit, strip CLAUDECODE/ORC_*, add fixture vars
	env := make([]string, 0, len(os.Environ())+len(vars)*2)
	for _, kv := range os.Environ() {
		idx := strings.IndexByte(kv, '=')
		if idx < 0 {
			continue
		}
		key := kv[:idx]
		if strings.HasPrefix(key, "CLAUDECODE") || strings.HasPrefix(key, "ORC_") {
			continue
		}
		env = append(env, kv)
	}
	for k, v := range vars {
		env = append(env, k+"="+v)
		env = append(env, "ORC_"+k+"="+v)
	}
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	runErr := cmd.Run()

	artifactsDir := state.ArtifactsDirForWorkflow(worktreePath, workflowName, ticket)

	// After a successful run, the runner archives artifacts to history/<run-id>/
	// and removes the originals. Detect this and load from the archive instead.
	loadDir := artifactsDir
	if runErr == nil && !state.HasState(artifactsDir) {
		if histDir, err := state.LatestHistoryDir(artifactsDir); err == nil && histDir != "" {
			loadDir = histDir
		}
	}

	costs, _ := state.LoadCosts(loadDir)
	timing, _ := state.LoadTiming(loadDir)

	status := "failed"
	if runErr == nil {
		if st, err := state.Load(loadDir); err == nil {
			status = st.GetStatus()
		} else {
			status = "completed"
		}
	}

	result := &RunResult{
		ArtifactsDir: loadDir,
		Status:       status,
		Err:          runErr,
	}
	if costs != nil {
		result.CostUSD = costs.TotalCostUSD
	}
	if timing != nil {
		result.DurationSeconds = timing.TotalElapsed().Seconds()
	}
	return result, nil
}

// LoadHistory reads the eval history from projectRoot/.orc/eval-history.json.
// Returns an empty History if the file does not exist.
func LoadHistory(projectRoot string) (*History, error) {
	path := filepath.Join(projectRoot, ".orc", "eval-history.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &History{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("eval: reading eval-history.json: %w", err)
	}
	var h History
	if err := json.Unmarshal(data, &h); err != nil {
		return nil, fmt.Errorf("eval: parsing eval-history.json: %w", err)
	}
	return &h, nil
}

// SaveHistory writes the eval history atomically.
func SaveHistory(projectRoot string, h *History) error {
	path := filepath.Join(projectRoot, ".orc", "eval-history.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("eval: creating .orc dir: %w", err)
	}
	data, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return fmt.Errorf("eval: marshaling history: %w", err)
	}
	return state.WriteFileAtomic(path, data, 0644)
}

// AppendResult adds a new history entry from the given case results.
func (h *History) AppendResult(fingerprint string, cases []CaseResult) {
	entry := HistoryEntry{
		Timestamp:         time.Now().UTC().Format(time.RFC3339),
		ConfigFingerprint: fingerprint,
		Cases:             make(map[string]CaseHistoryEntry),
	}
	for _, c := range cases {
		entry.Cases[c.Name] = CaseHistoryEntry{
			Score:           c.Score,
			CostUSD:         c.CostUSD,
			DurationSeconds: c.DurationSeconds,
			Details:         c.Details,
		}
	}
	h.Runs = append(h.Runs, entry)
}

// evalRunner scopes per-case execution so defer works correctly.
type evalRunner struct {
	projectRoot  string
	configPath   string
	workflowName string
	cfg          *config.Config
}

func (e *evalRunner) runCase(ctx context.Context, caseName string) (CaseResult, error) {
	caseDir := filepath.Join(e.projectRoot, ".orc", "evals", caseName)

	fixture, err := LoadFixture(caseDir)
	if err != nil {
		return CaseResult{Name: caseName}, fmt.Errorf("eval: loading fixture for %q: %w", caseName, err)
	}
	rubric, err := LoadRubric(caseDir, e.projectRoot)
	if err != nil {
		return CaseResult{Name: caseName}, fmt.Errorf("eval: loading rubric for %q: %w", caseName, err)
	}

	worktreePath, err := CreateWorktree(ctx, e.projectRoot, fixture.Ref, caseName)
	if err != nil {
		return CaseResult{Name: caseName}, fmt.Errorf("eval: creating worktree for %q: %w", caseName, err)
	}
	defer RemoveWorktree(e.projectRoot, worktreePath) //nolint:errcheck

	if err := copyOrcDir(e.projectRoot, worktreePath, e.configPath, e.cfg); err != nil {
		return CaseResult{Name: caseName}, fmt.Errorf("eval: copying .orc for %q: %w", caseName, err)
	}

	runResult, _ := RunWorkflow(ctx, worktreePath, fixture.Ticket, e.workflowName, fixture.Vars)
	criterionResults, rubricErr := EvaluateRubric(ctx, rubric, runResult.ArtifactsDir, worktreePath, e.projectRoot)
	if rubricErr != nil {
		fmt.Fprintf(os.Stderr, "  warning: rubric evaluation error for %q: %v\n", caseName, rubricErr)
	}
	score := ComputeScore(criterionResults, rubric)

	details := make(map[string]float64)
	var failures []string
	passCount := 0
	for _, cr := range criterionResults {
		details[cr.Name] = cr.Score
		if cr.Pass {
			passCount++
		} else {
			failures = append(failures, cr.Name+": "+cr.Detail)
		}
	}

	result := CaseResult{
		Name:            caseName,
		Score:           score,
		CostUSD:         runResult.CostUSD,
		DurationSeconds: runResult.DurationSeconds,
		PassCount:       passCount,
		TotalCount:      len(criterionResults),
		Failures:        failures,
		Details:         details,
	}
	if runResult.Err != nil {
		result.WorkflowErr = runResult.Err.Error()
	}
	return result, nil
}

// RunEval discovers eval cases, runs each sequentially, persists history, and returns results.
func RunEval(ctx context.Context, projectRoot, configPath, workflowName string, cfg *config.Config, caseName string) (string, []CaseResult, error) {
	allCases, err := DiscoverCases(projectRoot)
	if err != nil {
		return "", nil, err
	}

	cases := allCases
	if caseName != "" {
		found := false
		for _, c := range allCases {
			if c == caseName {
				found = true
				break
			}
		}
		if !found {
			return "", nil, fmt.Errorf("eval: case not found %q (available: %s)", caseName, strings.Join(allCases, ", "))
		}
		cases = []string{caseName}
	}

	fingerprint, err := ConfigFingerprint(configPath, cfg, projectRoot)
	if err != nil {
		return "", nil, fmt.Errorf("eval: computing fingerprint: %w", err)
	}

	fmt.Printf("\n  orc eval — %d cases, config fingerprint %s\n\n", len(cases), fingerprint)

	runner := &evalRunner{
		projectRoot:  projectRoot,
		configPath:   configPath,
		workflowName: workflowName,
		cfg:          cfg,
	}

	var results []CaseResult
	for _, name := range cases {
		if ctx.Err() != nil {
			break
		}
		result, err := runner.runCase(ctx, name)
		if err != nil {
			fmt.Printf("  warning: case %q failed: %v\n", name, err)
		}
		results = append(results, result)
	}

	history, err := LoadHistory(projectRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: cannot load eval history, skipping save: %v\n", err)
	} else {
		history.AppendResult(fingerprint, results)
		if err := SaveHistory(projectRoot, history); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: failed to save eval history: %v\n", err)
		}
	}

	return fingerprint, results, nil
}
