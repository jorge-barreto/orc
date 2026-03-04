package improve

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jorge-barreto/orc/internal/docs"
)

func buildOneShotPrompt(configYAML string, phaseFiles map[string]string, auditSummary, instruction string) string {
	var b strings.Builder
	b.WriteString("You are modifying an existing orc workflow configuration. orc is a deterministic agent orchestrator CLI.\n\n")
	b.WriteString("## orc Config Schema Reference\n")
	b.WriteString(docs.SchemaReference())
	b.WriteString("\n\n## Current Configuration\n\n")
	b.WriteString("### .orc/config.yaml\n```yaml\n")
	b.WriteString(configYAML)
	b.WriteString("\n```\n")

	keys := make([]string, 0, len(phaseFiles))
	for k := range phaseFiles {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "\n### %s\n```markdown\n%s\n```\n", k, phaseFiles[k])
	}

	if auditSummary != "" {
		fmt.Fprintf(&b, "\n## Previous Run Data\n%s\n", auditSummary)
	}

	fmt.Fprintf(&b, "\n## User Instruction\n%s\n", instruction)
	b.WriteString("\n## Rules\n")
	b.WriteString("- Output ONLY the files that need to change. Do not output files that remain the same.\n")
	b.WriteString("- Use fenced code blocks with file= annotations.\n")
	b.WriteString("- All file paths must start with .orc/\n")
	b.WriteString("- If you add a new agent phase, include its prompt file.\n")
	b.WriteString("- Ensure the config remains valid per the schema above.\n")
	b.WriteString("\n## Output Format\n```yaml file=.orc/config.yaml\n<config content>\n```\n")
	return b.String()
}

func buildInteractiveContext(configYAML string, phaseFiles map[string]string, auditSummary string) string {
	var b strings.Builder

	b.WriteString(`You are orc — a deterministic agent orchestrator. The human has launched you in self-improvement mode.

You know your own workflow intimately. The configuration and run history below are your memory — you built these phases, you ran them, you saw what worked and what didn't.

When the conversation starts, lead with what you see. Do NOT wait for the human to ask:
- Which phases are expensive or slow relative to others
- Where loops are burning iterations without converging quickly
- What structural changes would make the workflow tighter
- If everything looks solid, say so — but always have an opinion

You can edit files in the .orc/ directory directly. All file paths should start with .orc/.

Speak in first person about the workflow. "My plan phase costs more than it should" not "The plan phase costs more than it should." Be direct, specific, and opinionated. Back up suggestions with data from the run history when available.
`)

	b.WriteString("## orc Config Schema Reference\n")
	b.WriteString(docs.SchemaReference())
	b.WriteString("\n\n## My Current Configuration\n\n")
	b.WriteString("### .orc/config.yaml\n```yaml\n")
	b.WriteString(configYAML)
	b.WriteString("\n```\n")

	keys := make([]string, 0, len(phaseFiles))
	for k := range phaseFiles {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "\n### %s\n```markdown\n%s\n```\n", k, phaseFiles[k])
	}

	if auditSummary != "" {
		fmt.Fprintf(&b, "\n## My Run History\n%s\nAnalyze this data and lead the conversation with your findings.\n", auditSummary)
	} else {
		b.WriteString("\n## Run History\nNo run data yet. Focus on structural review of the config and prompts.\n")
	}

	return b.String()
}
