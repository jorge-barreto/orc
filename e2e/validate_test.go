//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestValidate covers the `orc validate` subcommand. No haiku spend —
// validate never dispatches agent phases.
//
// Table-driven: each case writes a config.yaml, runs `orc validate`,
// and asserts exit code + stdout/stderr content. The happy-path case
// expects exit 0 with the "Config valid" marker; failure cases expect
// exit 3 (ExitConfigError) with a specific substring in the error.
func TestValidate(t *testing.T) {
	cases := []struct {
		name              string
		yaml              string
		writePromptStub   bool   // if true, write a stub .orc/prompts/p.md for agent phases
		wantExitCode      int    // 0 = valid; 3 = ExitConfigError
		wantStdoutContain string // substring expected in stdout on success
		wantStderrContain string // substring expected in stderr on failure
	}{
		{
			name: "happy_path_minimal_script",
			yaml: `name: ok
phases:
  - name: greet
    type: script
    run: echo hi
`,
			wantExitCode:      0,
			wantStdoutContain: "Config valid",
		},
		{
			name: "missing_name",
			yaml: `phases:
  - name: a
    type: script
    run: echo hi
`,
			wantExitCode:      3,
			wantStderrContain: "'name' is required",
		},
		{
			name: "no_phases",
			yaml: `name: empty
phases: []
`,
			wantExitCode:      3,
			wantStderrContain: "at least one phase is required",
		},
		{
			name: "invalid_model",
			yaml: `name: bad-model
phases:
  - name: a
    type: agent
    prompt: .orc/prompts/p.md
    model: gpt-5
`,
			writePromptStub:   true,
			wantExitCode:      3,
			wantStderrContain: "unknown model",
		},
		{
			name: "invalid_effort",
			yaml: `name: bad-effort
phases:
  - name: a
    type: script
    run: echo hi
    effort: extreme
`,
			wantExitCode:      3,
			wantStderrContain: "unknown effort",
		},
		{
			name: "invalid_on_rate_limit",
			yaml: `name: bad-rl
on-rate-limit: retry
phases:
  - name: a
    type: script
    run: echo hi
`,
			wantExitCode:      3,
			wantStderrContain: "unknown on-rate-limit",
		},
		{
			name: "agent_prompt_missing",
			yaml: `name: no-prompt-file
phases:
  - name: a
    type: agent
    prompt: .orc/prompts/does-not-exist.md
    model: haiku
`,
			wantExitCode:      3,
			wantStderrContain: "prompt",
		},
		{
			name: "loop_goto_forward_reference",
			yaml: `name: bad-loop
phases:
  - name: first
    type: script
    run: echo hi
    loop:
      goto: second
      min: 1
      max: 2
  - name: second
    type: script
    run: echo later
`,
			wantExitCode:      3,
			wantStderrContain: "loop.goto",
		},
		{
			name: "parallel_with_and_loop_combined",
			yaml: `name: parloop
phases:
  - name: a
    type: script
    run: echo a
  - name: b
    type: script
    parallel-with: c
    run: echo b
    loop:
      goto: a
      min: 1
      max: 2
  - name: c
    type: script
    run: echo c
`,
			wantExitCode:      3,
			wantStderrContain: "parallel-with and loop",
		},
		{
			name: "invalid_ticket_pattern_regex",
			yaml: `name: bad-regex
ticket-pattern: "[unclosed"
phases:
  - name: a
    type: script
    run: echo hi
`,
			wantExitCode:      3,
			wantStderrContain: "ticket-pattern",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			w := NewWorkspace(t, tc.yaml)
			if tc.writePromptStub {
				w.WritePrompt("prompts/p.md", "stub")
			}

			res := w.RunOrc("validate")

			if res.ExitCode != tc.wantExitCode {
				t.Errorf("exit code = %d, want %d\nstdout: %s\nstderr: %s",
					res.ExitCode, tc.wantExitCode, res.Stdout, res.Stderr)
			}
			if tc.wantStdoutContain != "" && !strings.Contains(res.Stdout, tc.wantStdoutContain) {
				t.Errorf("stdout missing %q\ngot: %s", tc.wantStdoutContain, res.Stdout)
			}
			if tc.wantStderrContain != "" {
				combined := res.Stderr + res.Stdout
				if !strings.Contains(combined, tc.wantStderrContain) {
					t.Errorf("stderr/stdout missing %q\nstdout: %s\nstderr: %s",
						tc.wantStderrContain, res.Stdout, res.Stderr)
				}
			}

			// A validated config must not create the artifacts dir — validate
			// is purely a lint.
			if _, err := os.Stat(filepath.Join(w.Dir, ".orc", "artifacts")); err == nil {
				t.Errorf("validate created artifacts dir; it should not")
			}
		})
	}
}
