//go:build e2e

package e2e

import (
	"os"
	"strings"
	"testing"
)

// TestVars_ExpansionInScriptsAndPrompts verifies that built-in vars
// (TICKET, ARTIFACTS_DIR, WORK_DIR, PROJECT_ROOT) and custom vars from
// the config's `vars:` block are correctly expanded in both script `run:`
// commands and agent prompt templates. Also verifies that the rendered
// prompt file saved to artifacts contains expanded values (not raw
// $VARIABLE placeholders).
func TestVars_ExpansionInScriptsAndPrompts(t *testing.T) {
	if os.Getenv("CLAUDE_CODE_OAUTH_TOKEN") == "" {
		t.Skip("CLAUDE_CODE_OAUTH_TOKEN not set; skipping agent-phase test")
	}

	cfg := `name: vars
vars:
  GREETING: hello
  TARGET: world
  NESTED: $GREETING-$TARGET
phases:
  - name: script-vars
    type: script
    run: |
      {
        echo "TICKET=$TICKET"
        echo "ARTIFACTS_DIR=$ARTIFACTS_DIR"
        echo "WORK_DIR=$WORK_DIR"
        echo "PROJECT_ROOT=$PROJECT_ROOT"
        echo "GREETING=$GREETING"
        echo "TARGET=$TARGET"
        echo "NESTED=$NESTED"
        echo "ORC_TICKET=$ORC_TICKET"
        echo "ORC_GREETING=$ORC_GREETING"
      } > $ARTIFACTS_DIR/vars.txt

  - name: agent-vars
    type: agent
    prompt: .orc/prompts/vars-agent.md
    model: haiku
    outputs: [agent-vars.txt]

  - name: verify
    type: script
    run: |
      set -e
      grep -qx "TICKET=VARS-001" $ARTIFACTS_DIR/vars.txt
      grep -qx "GREETING=hello" $ARTIFACTS_DIR/vars.txt
      grep -qx "TARGET=world" $ARTIFACTS_DIR/vars.txt
      grep -qx "NESTED=hello-world" $ARTIFACTS_DIR/vars.txt
      grep -qx "ORC_TICKET=VARS-001" $ARTIFACTS_DIR/vars.txt
      grep -qx "ORC_GREETING=hello" $ARTIFACTS_DIR/vars.txt
      grep -Eq "^ARTIFACTS_DIR=.+/artifacts/VARS-001$" $ARTIFACTS_DIR/vars.txt
      grep -Eq "^PROJECT_ROOT=/" $ARTIFACTS_DIR/vars.txt
      test -f $ARTIFACTS_DIR/agent-vars.txt
`
	w := NewWorkspace(t, cfg)
	w.Ticket = "VARS-001"
	w.ArtifactsDir = strings.Replace(w.ArtifactsDir, "TEST-1", "VARS-001", 1)

	promptBytes, err := os.ReadFile("prompts/vars-agent.md")
	if err != nil {
		t.Fatalf("read prompt source: %v", err)
	}
	w.WritePrompt("prompts/vars-agent.md", string(promptBytes))

	res := w.RunOrc("run", w.Ticket, "--auto")

	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0\nstdout: %s\nstderr: %s",
			res.ExitCode, res.Stdout, res.Stderr)
	}

	rr := w.ReadRunResult()
	if got := rr["status"]; got != "completed" {
		t.Errorf("run-result.status = %v, want \"completed\"", got)
	}
	if got := rr["phases_completed"]; got != float64(3) {
		t.Errorf("run-result.phases_completed = %v, want 3", got)
	}
	if got := rr["ticket"]; got != "VARS-001" {
		t.Errorf("run-result.ticket = %v, want \"VARS-001\"", got)
	}

	rendered := w.ReadHistoryFile("prompts/phase-2.md")
	mustContain := []string{"TICKET=VARS-001", "GREETING=hello"}
	for _, s := range mustContain {
		if !strings.Contains(rendered, s) {
			t.Errorf("rendered prompt missing %q; got:\n%s", s, rendered)
		}
	}
	mustNotContain := []string{"$TICKET", "$GREETING", "$ARTIFACTS_DIR"}
	for _, s := range mustNotContain {
		if strings.Contains(rendered, s) {
			t.Errorf("rendered prompt still has literal placeholder %q; got:\n%s", s, rendered)
		}
	}

	agentOut := w.ReadHistoryFile("agent-vars.txt")
	agentWantContains := []string{"TICKET=VARS-001", "GREETING=hello"}
	for _, s := range agentWantContains {
		if !strings.Contains(agentOut, s) {
			t.Errorf("agent-vars.txt missing %q; got:\n%s", s, agentOut)
		}
	}
}
