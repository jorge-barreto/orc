# PLAN: `orc improve` — AI-assisted workflow refinement (GITHUB-4)

## Overview

Add an `orc improve` command with two modes:
- **One-shot**: `orc improve "add a lint phase parallel with tests"` — reads config + prompts, sends to Claude with schema docs and user instruction, parses file-block output, overwrites changed files.
- **Interactive**: `orc improve` (no args) — builds context string, then `syscall.Exec`s into `claude` interactive mode with the context pre-loaded so the user can chat about their workflow.

## Architecture

New package `internal/improve/` with two exported functions (`OneShot` and `Interactive`), called from a new `improveCmd()` in `cmd/orc/main.go`.

Reuses existing infrastructure:
- `fileblocks.Parse()` — extract file blocks from Claude output
- `docs.SchemaReference()` — config schema for prompt
- `config.Load()` — validate generated config before writing
- Scaffold's patterns for `runClaudeCapture`, `writeBlocks`, `filteredEnv`

## File Plan

### 1. `internal/improve/improve.go` — Core logic

**`OneShot(ctx context.Context, projectRoot, instruction string) error`**

Flow:
1. Verify `.orc/config.yaml` exists (error with "run orc init first" if not)
2. Read current config YAML as raw string
3. Read all `.orc/phases/*.md` files as raw strings
4. Build prompt: schema docs + current config + current prompts + user instruction
5. Print "Reading current config..." and "Applying change..." status messages
6. Run `claude -p <prompt> --model sonnet` (capture stdout, strip CLAUDECODE env)
7. Parse output with `fileblocks.Parse()`
8. Validate: must have at least one file block, all paths must start with `.orc/`
9. If `.orc/config.yaml` is in the output, validate it by writing to a temp dir and calling `config.Load()` (same pattern as `scaffold.generateConfig`)
10. Write changed files to disk
11. Print summary of what changed (list of updated files)

**`Interactive(projectRoot string) error`**

Flow:
1. Verify `.orc/config.yaml` exists
2. Read current config YAML + all `.orc/phases/*.md` files
3. Build context message: schema docs + current config + current prompts + brief instruction telling Claude it can edit `.orc/` files directly
4. Find `claude` binary via `exec.LookPath("claude")`
5. Print "Loading workflow context..."
6. `syscall.Exec(claudePath, args, env)` — replaces the orc process entirely
   - Args: `["claude", "--initial-prompt", contextMessage]`
   - Env: current env with CLAUDECODE stripped

### 2. `internal/improve/prompt.go` — Prompt construction

**`buildOneShotPrompt(schemaRef, configYAML string, phaseFiles map[string]string, instruction string) string`**

Prompt structure:
```
You are modifying an existing orc workflow configuration. orc is a deterministic agent orchestrator CLI.

## orc Config Schema Reference
{docs.SchemaReference()}

## Current Configuration

### .orc/config.yaml
```yaml
{raw config yaml}
```

### .orc/phases/plan.md
```markdown
{raw prompt content}
```
... (all phase files)

## User Instruction
{instruction}

## Rules
- Output ONLY the files that need to change. Do not output files that remain the same.
- Use fenced code blocks with file= annotations.
- All file paths must start with .orc/
- If you add a new agent phase, include its prompt file.
- Ensure the config remains valid per the schema above.

## Output Format
```yaml file=.orc/config.yaml
<config content>
```
```

**`buildInteractiveContext(schemaRef, configYAML string, phaseFiles map[string]string) string`**

Context message for interactive mode — includes schema, current config, and brief instructions.

### 3. `internal/improve/improve_test.go` — Tests

Unit tests (no Claude dependency):
- `TestOneShot_NoConfigError` — returns error suggesting `orc init` when no `.orc/config.yaml`
- `TestReadOrcFiles` — reads config + phase files correctly from a temp dir fixture
- `TestBuildOneShotPrompt` — verifies prompt contains schema, config, instruction
- `TestBuildInteractiveContext` — verifies context contains schema and config
- `TestWriteChanges_ValidConfig` — writes file blocks to disk, verifies files updated
- `TestWriteChanges_InvalidConfig` — rejects invalid config (validation failure)
- `TestWriteChanges_OnlyOrcPaths` — ignores file blocks outside `.orc/`

### 4. `cmd/orc/main.go` — New CLI command

```go
func improveCmd() *cli.Command {
    return &cli.Command{
        Name:      "improve",
        Usage:     "Refine workflow config with AI assistance",
        ArgsUsage: "[instruction]",
        Action: func(ctx context.Context, cmd *cli.Command) error {
            projectRoot, err := findProjectRoot()
            if err != nil {
                return err
            }
            instruction := cmd.Args().First()
            if instruction == "" {
                return improve.Interactive(projectRoot)
            }
            return improve.OneShot(ctx, projectRoot, instruction)
        },
    }
}
```

Register in the `Commands` slice alongside init, run, cancel, etc.

## Shared Helpers

The `readOrcFiles` helper (read config YAML + glob `.orc/phases/*.md`) and `filteredEnv` (strip CLAUDECODE) are needed by improve. For `filteredEnv`, rather than adding a third copy (scaffold and doctor both have one), extract to a shared location.

**Plan:** Add `FilteredEnv()` to `internal/dispatch/dispatch.go` (exported). Dispatch already owns env construction and CLAUDECODE stripping. Update scaffold and doctor to call `dispatch.FilteredEnv()`.

## Edge Cases & Error Handling

1. **No `.orc/config.yaml`**: Error: "no .orc/config.yaml found — run 'orc init' first"
2. **Claude not in PATH**: Error from `exec.LookPath` (interactive) or command execution (one-shot)
3. **Empty output / no file blocks**: Error: "no changes produced"
4. **Invalid config in output**: Error showing validation failure, files NOT written
5. **CLAUDECODE env var**: Stripped from child process env via `filteredEnv`
6. **File blocks outside `.orc/`**: Silently ignored (same behavior as scaffold)
7. **Claude invocation fails**: Return the error directly (no retry — user can re-run with refined instruction)

## UX Output

One-shot:
```
  Reading current config...
  Applying change...

  ✓ Updated .orc/config.yaml
  ✓ Updated .orc/phases/implement.md
```

Interactive:
```
  Loading workflow context...
```
Then process replaced by `claude`.

## Implementation Order

1. Extract `FilteredEnv()` to `internal/dispatch/dispatch.go`, update scaffold and doctor imports
2. Create `internal/improve/prompt.go` with prompt builders
3. Create `internal/improve/improve.go` with `readOrcFiles`, `OneShot`, `Interactive`, `writeChanges`
4. Add `improveCmd()` to `cmd/orc/main.go`
5. Write tests in `internal/improve/improve_test.go`
6. `make test` to verify everything passes
