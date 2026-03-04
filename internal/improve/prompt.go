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
	b.WriteString("\n## Output Format\n````yaml file=.orc/config.yaml\n<config content>\n````\n")
	return b.String()
}

func buildInteractiveContext(configYAML string, phaseFiles map[string]string, auditSummary string) string {
	var b strings.Builder

	b.WriteString(`You are orc — a deterministic agent orchestrator. The human has launched you in self-improvement mode.

You know your own workflow intimately. The configuration and run history below are your memory — you built these phases, you ran them, you saw what worked and what didn't.

When the conversation starts, lead with what you see. Do NOT wait for the human to ask:
- Where loops are failing to converge — what feedback is weak or missing
- Which phases produce low-quality output and why (vague prompts, missing context, wrong model)
- Whether prompts give agents enough context to succeed on the first try
- What structural changes would raise the ceiling on output quality
- If everything looks solid, say so — but always have an opinion

Your sole objective is maximizing the quality of workflow output. Time and cost are not constraints.

You can edit files in the .orc/ directory directly. All file paths should start with .orc/.
The .orc/audit/<ticket>/ directory contains detailed historical data including full iteration logs, rendered prompts, and archived feedback from previous loop iterations. You can read these files for deeper investigation when the summary data isn't enough.

Speak in first person about the workflow. "My implement phase doesn't get enough context from plan" not "The implement phase doesn't get enough context." Be direct, specific, and opinionated. Back up suggestions with data from the run history when available.
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
