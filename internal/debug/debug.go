package debug

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/dispatch"
	"github.com/jorge-barreto/orc/internal/state"
	"github.com/jorge-barreto/orc/internal/ux"
)

// ToolCall holds the name and summary of a tool invocation parsed from a log file.
type ToolCall struct {
	Name    string
	Summary string
}

// OutputFile describes a declared phase output artifact.
type OutputFile struct {
	Name   string
	Size   int64
	Exists bool
}

// FeedbackFile describes a feedback file from a prior phase.
type FeedbackFile struct {
	FromPhase string
	Size      int64
}

// PhaseInfo holds all gathered information about a phase execution.
type PhaseInfo struct {
	Name, Type, Model, Effort                         string
	Index, Total                                      int
	Duration                                          time.Duration
	CostUSD                                           float64
	InputTokens, OutputTokens, CacheRead, CacheCreate int
	PromptPath                                        string
	PromptSize                                        int64
	Variables                                         map[string]string
	ToolCalls                                         []ToolCall
	Outputs                                           []OutputFile
	FeedbackFiles                                     []FeedbackFile
	ExitStatus                                        string
	Attempts                                          int
	RunCommand                                        string
}

// parseToolCalls reads a log file and extracts tool call lines starting with "⚡ ".
// Returns nil (not empty slice) when no matches are found.
func parseToolCalls(logPath string) ([]ToolCall, error) {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	var calls []ToolCall
	for _, line := range lines {
		if !strings.HasPrefix(line, "⚡ ") {
			continue
		}
		rest := strings.TrimPrefix(line, "⚡ ")
		name, summary, _ := strings.Cut(rest, " ")
		calls = append(calls, ToolCall{Name: name, Summary: summary})
	}
	if len(calls) == 0 {
		return nil, nil
	}
	return calls, nil
}

// FindMostRecentTicket finds the ticket directory with the most recently modified state.json.
// If workflow is empty, searches under projectRoot/.orc/artifacts/.
// If workflow is set, searches under projectRoot/.orc/artifacts/<workflow>/.
func FindMostRecentTicket(projectRoot, workflow string) (string, error) {
	var baseDir string
	if workflow == "" {
		baseDir = filepath.Join(projectRoot, ".orc", "artifacts")
	} else {
		baseDir = filepath.Join(projectRoot, ".orc", "artifacts", workflow)
	}

	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return "", fmt.Errorf("no tickets found — specify a ticket argument")
	}

	var latestName string
	var latestMtime time.Time
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		statePath := filepath.Join(baseDir, e.Name(), "state.json")
		info, err := os.Stat(statePath)
		if err != nil {
			continue
		}
		if info.ModTime().After(latestMtime) {
			latestMtime = info.ModTime()
			latestName = e.Name()
		}
	}
	if latestName == "" {
		return "", fmt.Errorf("no tickets found — specify a ticket argument")
	}
	return latestName, nil
}

// readFeedbackFiles reads feedback files from the feedback subdirectory of artifactsDir.
// Returns nil if the directory doesn't exist or no matching files are found.
func readFeedbackFiles(artifactsDir string) []FeedbackFile {
	feedbackDir := filepath.Join(artifactsDir, "feedback")
	entries, err := os.ReadDir(feedbackDir)
	if err != nil {
		return nil
	}
	var files []FeedbackFile
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "from-") || !strings.HasSuffix(name, ".md") {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Size() == 0 {
			continue
		}
		phase := strings.TrimPrefix(name, "from-")
		phase = strings.TrimSuffix(phase, ".md")
		files = append(files, FeedbackFile{FromPhase: phase, Size: info.Size()})
	}
	if len(files) == 0 {
		return nil
	}
	return files
}

// formatTokenCount formats a token count as a human-readable string.
func formatTokenCount(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1_000_000 {
		k := (n + 500) / 1000
		return fmt.Sprintf("%dK", k)
	}
	return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
}

// formatSize formats a byte count as a human-readable string.
func formatSize(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
}

// scanLogForStatus reads the log file backwards looking for exit status markers.
func scanLogForStatus(logPath, phaseName string) string {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		prefix := fmt.Sprintf(`[orc] phase "%s" failed: `, phaseName)
		if strings.Contains(line, prefix) {
			idx := strings.Index(line, prefix)
			reason := line[idx+len(prefix):]
			return "failed: " + reason
		}
		if strings.Contains(line, "[orc] phase interrupted:") {
			return "interrupted"
		}
	}
	return ""
}

// determineExitStatus determines the exit status of a phase using state.json and log markers.
func determineExitStatus(logPath, artifactsDir string, phaseIdx int, phaseName string) string {
	logReason := scanLogForStatus(logPath, phaseName)
	if !state.HasState(artifactsDir) {
		if logReason != "" {
			return logReason
		}
		return "unknown"
	}
	st, err := state.Load(artifactsDir)
	if err != nil {
		if logReason != "" {
			return logReason
		}
		return "unknown"
	}
	switch {
	case st.PhaseIndex > phaseIdx:
		return "success"
	case st.PhaseIndex == phaseIdx:
		switch st.Status {
		case state.StatusCompleted:
			return "success"
		case state.StatusFailed:
			if logReason != "" {
				return logReason
			}
			return "failed"
		case state.StatusInterrupted:
			return "interrupted"
		case state.StatusRunning:
			return "in progress or stale"
		}
	}
	if logReason != "" {
		return logReason
	}
	return "unknown"
}

// Run gathers phase execution info and renders it to stdout.
func Run(projectRoot string, cfg *config.Config, phaseIdx int, ticket, workflow string) error {
	artifactsDir := state.ArtifactsDirForWorkflow(projectRoot, workflow, ticket)
	auditDir := state.AuditDirForWorkflow(projectRoot, workflow, ticket)
	phase := cfg.Phases[phaseIdx]

	logPath := state.LogPath(artifactsDir, phaseIdx)
	if _, err := os.Stat(logPath); err != nil {
		return fmt.Errorf("no log file found for phase %d (%s) — has this phase been executed for ticket %s?",
			phaseIdx+1, phase.Name, ticket)
	}

	// Load timing — try auditDir first, fallback to artifactsDir
	var timingEntry *state.TimingEntry
	timing, err := state.LoadTiming(auditDir)
	if err != nil || len(timing.Entries) == 0 {
		timing, _ = state.LoadTiming(artifactsDir)
	}
	if timing != nil {
		for i := len(timing.Entries) - 1; i >= 0; i-- {
			if timing.Entries[i].Phase == phase.Name {
				e := timing.Entries[i]
				timingEntry = &e
				break
			}
		}
	}

	// Load costs — try auditDir first, fallback to artifactsDir
	var costEntry *state.CostEntry
	costs, err := state.LoadCosts(auditDir)
	if err != nil || len(costs.Phases) == 0 {
		costs, _ = state.LoadCosts(artifactsDir)
	}
	if costs != nil {
		for i := len(costs.Phases) - 1; i >= 0; i-- {
			if costs.Phases[i].Name == phase.Name {
				e := costs.Phases[i]
				costEntry = &e
				break
			}
		}
	}

	// Load attempt counts
	attemptCounts, _ := state.LoadAttemptCounts(auditDir)
	attempts := attemptCounts[phaseIdx]

	// Prompt path and size (agent only)
	var promptPath string
	var promptSize int64
	if phase.Type == "agent" {
		pp := state.PromptPath(artifactsDir, phaseIdx)
		if info, err := os.Stat(pp); err == nil {
			promptPath = pp
			promptSize = info.Size()
		}
	}

	// Build env + vars
	env := &dispatch.Environment{
		ProjectRoot:  projectRoot,
		WorkDir:      projectRoot,
		ArtifactsDir: artifactsDir,
		Ticket:       ticket,
		Workflow:     workflow,
	}
	vars := env.Vars()
	if len(cfg.Vars) > 0 {
		env.CustomVars = dispatch.ExpandConfigVars(cfg.Vars, vars)
		vars = env.Vars()
	}

	// Parse tool calls (agent only)
	var toolCalls []ToolCall
	if phase.Type == "agent" {
		toolCalls, _ = parseToolCalls(logPath)
	}

	// Declared outputs
	var outputs []OutputFile
	for _, name := range phase.Outputs {
		of := OutputFile{Name: name}
		if info, err := os.Stat(filepath.Join(artifactsDir, name)); err == nil {
			of.Exists = true
			of.Size = info.Size()
		}
		outputs = append(outputs, of)
	}

	// Feedback files
	feedbackFiles := readFeedbackFiles(artifactsDir)

	// Exit status
	exitStatus := determineExitStatus(logPath, artifactsDir, phaseIdx, phase.Name)

	// Duration from timing
	var duration time.Duration
	if timingEntry != nil && !timingEntry.End.IsZero() {
		duration = timingEntry.End.Sub(timingEntry.Start)
	}

	// Cost data
	var costUSD float64
	var inputTokens, outputTokens, cacheRead, cacheCreate int
	if costEntry != nil {
		costUSD = costEntry.CostUSD
		inputTokens = costEntry.InputTokens
		outputTokens = costEntry.OutputTokens
		cacheRead = costEntry.CacheReadInputTokens
		cacheCreate = costEntry.CacheCreationInputTokens
	}

	info := &PhaseInfo{
		Name:          phase.Name,
		Type:          phase.Type,
		Model:         phase.Model,
		Effort:        phase.Effort,
		Index:         phaseIdx,
		Total:         len(cfg.Phases),
		Duration:      duration,
		CostUSD:       costUSD,
		InputTokens:   inputTokens,
		OutputTokens:  outputTokens,
		CacheRead:     cacheRead,
		CacheCreate:   cacheCreate,
		PromptPath:    promptPath,
		PromptSize:    promptSize,
		Variables:     vars,
		ToolCalls:     toolCalls,
		Outputs:       outputs,
		FeedbackFiles: feedbackFiles,
		ExitStatus:    exitStatus,
		Attempts:      attempts,
		RunCommand:    phase.Run,
	}

	render(os.Stdout, info)
	return nil
}

// render writes the phase debug output to w.
func render(w io.Writer, info *PhaseInfo) {
	// Phase header
	typeStr := info.Type
	if info.Type == "agent" && info.Model != "" {
		typeStr += "/" + info.Model
		if info.Effort != "" && info.Effort != "default" {
			typeStr += ", " + info.Effort
		}
	}
	attemptSuffix := ""
	if info.Attempts > 1 {
		attemptSuffix = fmt.Sprintf(" %s(attempt %d)%s", ux.Dim, info.Attempts, ux.Reset)
	}
	fmt.Fprintf(w, "%sPhase:%s %s (%s)%s\n", ux.Bold, ux.Reset, info.Name, typeStr, attemptSuffix)

	// Duration line
	fmt.Fprintf(w, "%sDuration:%s %s", ux.Bold, ux.Reset, state.FormatDuration(info.Duration))
	if info.Type == "agent" {
		fmt.Fprintf(w, " | Cost: $%.2f | Tokens: %s in / %s out",
			info.CostUSD, formatTokenCount(info.InputTokens), formatTokenCount(info.OutputTokens))
	}
	fmt.Fprintln(w)

	// Blank line after header
	fmt.Fprintln(w)

	// Agent-only sections
	if info.Type == "agent" {
		if info.PromptPath != "" {
			fmt.Fprintf(w, "Rendered prompt: %s (%s)\n", info.PromptPath, formatSize(info.PromptSize))
		}

		// Variables: sorted keys, KEY=value, truncate values > 60 chars
		keys := make([]string, 0, len(info.Variables))
		for k := range info.Variables {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := info.Variables[k]
			if len(v) > 60 {
				v = v[:60] + "..."
			}
			fmt.Fprintf(w, "%s=%s\n", k, v)
		}
		fmt.Fprintln(w)

		// Tool calls
		n := len(info.ToolCalls)
		fmt.Fprintf(w, "Tool calls (%d):\n", n)
		if n <= 20 {
			for _, tc := range info.ToolCalls {
				fmt.Fprintf(w, "  %s %s\n", tc.Name, tc.Summary)
			}
		} else {
			for _, tc := range info.ToolCalls[:15] {
				fmt.Fprintf(w, "  %s %s\n", tc.Name, tc.Summary)
			}
			fmt.Fprintf(w, "  ... (%d total)\n", n)
			for _, tc := range info.ToolCalls[n-5:] {
				fmt.Fprintf(w, "  %s %s\n", tc.Name, tc.Summary)
			}
		}
		fmt.Fprintln(w)
	}

	// Script-only
	if info.Type == "script" && info.RunCommand != "" {
		fmt.Fprintf(w, "Command: %s\n\n", info.RunCommand)
	}

	// Outputs (all phase types)
	fmt.Fprintf(w, "Artifacts written:\n")
	if len(info.Outputs) == 0 {
		fmt.Fprintf(w, "  none declared\n")
	} else {
		for _, o := range info.Outputs {
			if o.Exists {
				fmt.Fprintf(w, "  %s (%s)\n", o.Name, formatSize(o.Size))
			} else {
				fmt.Fprintf(w, "  %s (missing)\n", o.Name)
			}
		}
	}

	// Feedback
	fmt.Fprintf(w, "Feedback:\n")
	if len(info.FeedbackFiles) == 0 {
		fmt.Fprintf(w, "  none\n")
	} else {
		for _, f := range info.FeedbackFiles {
			fmt.Fprintf(w, "  from %s (%s)\n", f.FromPhase, formatSize(f.Size))
		}
	}

	// Exit status (always last)
	exitColor := ux.Yellow
	if strings.HasPrefix(info.ExitStatus, "success") {
		exitColor = ux.Green
	}
	if strings.HasPrefix(info.ExitStatus, "failed") {
		exitColor = ux.Red
	}
	fmt.Fprintf(w, "\n%sExit:%s %s%s%s\n", ux.Bold, ux.Reset, exitColor, info.ExitStatus, ux.Reset)
}
