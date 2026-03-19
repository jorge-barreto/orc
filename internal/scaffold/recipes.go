package scaffold

import "fmt"

// Recipe describes a named scaffold template with config and phase files.
type Recipe struct {
	Name        string
	Description string
	Workflow    string
	Files       map[string]string // relative path → content
}

const reviewPrompt = `You are a review agent for ticket $TICKET.

## Instructions

### Step 0: Reset review state

` + "```" + `bash
rm -f "$ARTIFACTS_DIR/review-pass.txt"
` + "```" + `

### Step 1: Gather context

- Read $ARTIFACTS_DIR/plan.md to understand the intended changes.
- Check for CLAUDE.md or CONTRIBUTING.md at $PROJECT_ROOT for project conventions.
- If $ARTIFACTS_DIR/review-findings.md exists, read it for prior review feedback.

### Step 2: Calibrate rigor

Check $ARTIFACTS_DIR/loop-counts.json for the current iteration count.

- First iteration: apply maximum rigor — check everything.
- Second iteration: verify that all issues from the previous review have been addressed.
- Third iteration and beyond: apply the convergence rule — only block on new issues introduced since the last review. Do not re-raise resolved issues.

### Step 3: Review the implementation

Run:
` + "```" + `bash
git log --oneline -10
` + "```" + `

Examine each changed file. Check for:
- Correctness: does the code do what the plan says?
- Robustness: are error cases handled?
- Security: no injections, no sensitive data exposure, no OWASP top-10 issues.
- Conventions: matches existing code style and project conventions.
- Scope: no unrelated changes or premature abstractions.

### Step 4: Run the test suite

Detect the build system and run tests:
- If Makefile exists: ` + "`make test`" + `
- If package.json exists: ` + "`npm test`" + `
- If go.mod exists: ` + "`go test ./...`" + `

Record the result.

### Step 5: Write review findings

Write $ARTIFACTS_DIR/review-findings.md with the following sections:

` + "```" + `markdown
# Review Findings

## Blocking Issues
<!-- List issues that must be fixed before this can be merged. -->
<!-- If none, write "None." -->

## Suggestions
<!-- Non-blocking improvements or observations. -->
<!-- If none, write "None." -->

## Verdict
<!-- PASS or FAIL, with one-line summary. -->
` + "```" + `

### Step 6: Signal pass or fail

- If there are **zero blocking issues**: ` + "`echo \"PASS\" > \"$ARTIFACTS_DIR/review-pass.txt\"`" + `
- If there are **any blocking issues**: do NOT write review-pass.txt.
`

// AllRecipes returns all built-in scaffold recipes.
func AllRecipes() []Recipe {
	return []Recipe{
		simpleRecipe(),
		standardRecipe(),
		fullPipelineRecipe(),
		reviewLoopRecipe(),
	}
}

// GetRecipe returns the named recipe or an error listing available names.
func GetRecipe(name string) (Recipe, error) {
	for _, r := range AllRecipes() {
		if r.Name == name {
			return r, nil
		}
	}
	return Recipe{}, fmt.Errorf("unknown recipe %q: available recipes are simple, standard, full-pipeline, review-loop", name)
}

func simpleRecipe() Recipe {
	return Recipe{
		Name:        "simple",
		Description: "Minimal plan-implement-review workflow",
		Workflow:    "plan → implement → review",
		Files: map[string]string{
			".orc/config.yaml":         fallbackConfig,
			".orc/phases/plan.md":      fallbackPlanPrompt,
			".orc/phases/implement.md": fallbackImplementPrompt,
		},
	}
}

func standardRecipe() Recipe {
	const cfg = `name: my-project

phases:
  - name: plan
    type: agent
    description: Analyze the ticket and produce an implementation plan
    prompt: .orc/phases/plan.md
    outputs:
      - plan.md

  - name: review-plan
    type: gate
    description: Review the plan before implementation

  - name: implement
    type: agent
    description: Implement the changes following the plan
    prompt: .orc/phases/implement.md

  - name: test
    type: script
    description: Run the test suite
    run: make test
    loop:
      goto: implement
      max: 4

  - name: review
    type: gate
    description: Human reviews the implementation
    loop:
      goto: implement
      max: 3
`
	return Recipe{
		Name:        "standard",
		Description: "Plan-gate-implement-test-review workflow with test loop",
		Workflow:    "plan → review-plan → implement → test → review",
		Files: map[string]string{
			".orc/config.yaml":         cfg,
			".orc/phases/plan.md":      fallbackPlanPrompt,
			".orc/phases/implement.md": fallbackImplementPrompt,
		},
	}
}

func fullPipelineRecipe() Recipe {
	const cfg = `name: my-project

phases:
  - name: plan
    type: agent
    description: Analyze the ticket and produce an implementation plan
    prompt: .orc/phases/plan.md
    outputs:
      - plan.md

  - name: review-plan
    type: gate
    description: Review the plan before implementation

  - name: implement
    type: agent
    description: Implement the changes following the plan
    prompt: .orc/phases/implement.md

  - name: test
    type: script
    description: Run the test suite
    run: make test
    loop:
      goto: implement
      max: 4

  - name: review
    type: agent
    description: AI self-review of the implementation
    prompt: .orc/phases/review.md
    loop:
      goto: implement
      min: 1
      max: 3
      check: test -f "$ARTIFACTS_DIR/review-pass.txt"

  - name: final-review
    type: gate
    description: Final human review
    loop:
      goto: implement
      max: 2
`
	return Recipe{
		Name:        "full-pipeline",
		Description: "Full pipeline with AI self-review loop and human gates",
		Workflow:    "plan → review-plan → implement → test → review → final-review",
		Files: map[string]string{
			".orc/config.yaml":         cfg,
			".orc/phases/plan.md":      fallbackPlanPrompt,
			".orc/phases/implement.md": fallbackImplementPrompt,
			".orc/phases/review.md":    reviewPrompt,
		},
	}
}

func reviewLoopRecipe() Recipe {
	const cfg = `name: my-project

phases:
  - name: plan
    type: agent
    description: Analyze the ticket and produce an implementation plan
    prompt: .orc/phases/plan.md
    outputs:
      - plan.md

  - name: implement
    type: agent
    description: Implement the changes following the plan
    prompt: .orc/phases/implement.md

  - name: review
    type: agent
    description: AI self-review of the implementation
    prompt: .orc/phases/review.md
    loop:
      goto: implement
      min: 3
      max: 5
      check: test -f "$ARTIFACTS_DIR/review-pass.txt"
`
	return Recipe{
		Name:        "review-loop",
		Description: "Convergent AI review loop, no gates, no test script",
		Workflow:    "plan → implement → review",
		Files: map[string]string{
			".orc/config.yaml":         cfg,
			".orc/phases/plan.md":      fallbackPlanPrompt,
			".orc/phases/implement.md": fallbackImplementPrompt,
			".orc/phases/review.md":    reviewPrompt,
		},
	}
}
