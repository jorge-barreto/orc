# TODO

## Interactive mode via Claude Code SDK subprocess API

Run agent phases using the JSON-based subprocess protocol instead of `claude -p`. `orc` would receive tool-use requests programmatically, display permission prompts to the user, and forward approvals back. This enables an `--interactive` flag where users can stream agent output and approve permissions in real-time.
