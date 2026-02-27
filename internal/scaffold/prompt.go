package scaffold

import "github.com/jorge-barreto/orc/internal/docs"

// buildInitPrompt constructs the full prompt for AI-powered init.
// The projectContext string is the rendered output of contextgather.Render().
func buildInitPrompt(projectContext string) string {
	return initPromptPrefix + docs.SchemaReference() + initPromptMiddle + projectContext + initPromptSuffix
}

const initPromptPrefix = `You are generating an orc workflow configuration for a software project. orc is a deterministic agent orchestrator CLI that runs AI workflows as a state machine.

Your job: analyze the project context below and generate a tailored workflow config plus prompt template files.

## orc Config Schema Reference

`

const initPromptMiddle = `

## Example Configs

### Example 1: Go backend service

` + "```" + `yaml file=.orc/config.yaml
name: my-go-service
ticket-pattern: '[A-Z]+-\d+'

default-allow-tools:
  - Bash

phases:
  - name: plan
    type: agent
    description: Analyze the ticket and produce an implementation plan
    prompt: .orc/phases/plan.md
    model: opus
    outputs:
      - plan.md

  - name: review-plan
    type: gate
    description: Human reviews the plan before implementation

  - name: implement
    type: agent
    description: Implement the changes following the plan
    prompt: .orc/phases/implement.md
    model: opus

  - name: test
    type: script
    description: Run tests
    run: go test ./... -count=1
    on-fail:
      goto: implement
      max: 3

  - name: lint
    type: script
    description: Run linter
    run: golangci-lint run ./...
    parallel-with: test

  - name: review
    type: gate
    description: Final human review before merging
` + "```" + `

### Example 2: Node.js / TypeScript project

` + "```" + `yaml file=.orc/config.yaml
name: my-ts-app
ticket-pattern: '[A-Z]+-\d+'

phases:
  - name: plan
    type: agent
    description: Analyze the ticket and produce an implementation plan
    prompt: .orc/phases/plan.md
    model: opus
    outputs:
      - plan.md

  - name: review-plan
    type: gate
    description: Human reviews the plan

  - name: implement
    type: agent
    description: Implement the changes
    prompt: .orc/phases/implement.md
    model: opus

  - name: test
    type: script
    description: Run tests
    run: npm test
    on-fail:
      goto: implement
      max: 3

  - name: lint
    type: script
    description: Run linter
    run: npm run lint
    parallel-with: test

  - name: review
    type: gate
    description: Final review
` + "```" + `

### Example 3: Python project

` + "```" + `yaml file=.orc/config.yaml
name: my-python-project
ticket-pattern: '[A-Z]+-\d+'

phases:
  - name: plan
    type: agent
    description: Analyze the ticket and create an implementation plan
    prompt: .orc/phases/plan.md
    model: opus
    outputs:
      - plan.md

  - name: review-plan
    type: gate
    description: Human reviews the plan

  - name: implement
    type: agent
    description: Implement the changes following the plan
    prompt: .orc/phases/implement.md
    model: opus

  - name: test
    type: script
    description: Run tests
    run: pytest
    on-fail:
      goto: implement
      max: 3

  - name: review
    type: gate
    description: Final review
` + "```" + `

## Project Context

`

const initPromptSuffix = `

## Instructions

Based on the project context above, generate a complete orc workflow. Produce:

1. A ` + "`.orc/config.yaml`" + ` tailored to this project. Follow this default workflow shape and adapt it:
   - **plan** (agent) — Analyze the ticket and produce a plan. Output: plan.md.
   - **review-plan** (gate) — Human reviews the plan.
   - **implement** (agent) — Implement the changes following the plan.
   - **test** (script) — Run the project's test suite. Detect the correct test command from the project files (e.g. ` + "`go test ./...`" + ` for Go, ` + "`npm test`" + ` for Node, ` + "`pytest`" + ` for Python, ` + "`make test`" + ` if a Makefile exists). Add on-fail with goto: implement, max: 3.
   - If the project has a linter, add a **lint** (script) phase with ` + "`parallel-with: test`" + `.
   - **review** (gate) — Final human review.

   Use ` + "`opus`" + ` as the model for agent phases. Set a reasonable ticket-pattern based on any conventions you see.

   **Tool permissions:** Agent phases have these tools auto-approved by default: Read, Edit, Write, Glob, Grep, Task, WebFetch, WebSearch. Bash is NOT auto-approved. If the workflow benefits from agents running shell commands (common), add ` + "`Bash`" + ` to ` + "`default-allow-tools`" + ` at the top level. For project-specific tools (e.g. MCP servers), use glob patterns like ` + "`mcp__atlassian__*`" + `. Per-phase ` + "`allow-tools`" + ` can override for individual phases.

2. Prompt template files for each agent phase. Each prompt should:
   - Reference ` + "`$TICKET`" + `, ` + "`$ARTIFACTS_DIR`" + `, ` + "`$PROJECT_ROOT`" + ` where appropriate.
   - Reference the project's actual structure, conventions, and build tools.
   - For the plan phase: instruct the agent to read the ticket, explore the codebase, and write a plan to ` + "`$ARTIFACTS_DIR/plan.md`" + `.
   - For the implement phase: instruct the agent to read ` + "`$ARTIFACTS_DIR/plan.md`" + ` and implement the changes.

## Output Format

Produce ONLY fenced code blocks with ` + "`file=`" + ` annotations. No explanation or text outside the code blocks.
Each block specifies its path relative to the project root:

` + "```" + `yaml file=.orc/config.yaml
<config content>
` + "```" + `

` + "```" + `markdown file=.orc/phases/plan.md
<prompt content>
` + "```" + `

All file paths MUST start with ` + "`.orc/`" + `.
`

const retryFeedback = `

IMPORTANT: Your previous attempt failed with this error: %v

Try again. Output ONLY fenced code blocks with file= annotations. One of them MUST be .orc/config.yaml. Ensure all agent phases reference prompt files that you also generate as file blocks.`
