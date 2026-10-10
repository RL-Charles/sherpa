package hooks

import "io"

// codex adapts OpenAI Codex CLI hooks (.codex/hooks.json, Codex ≥ 0.124).
// The payload and responses follow Claude Code's shape; file edits arrive as
// apply_patch calls whose paths are inside the patch text.
// https://learn.chatgpt.com/docs/hooks.md
type codex struct{}

func init() { Adapters["codex"] = codex{} }

// CodexEditTools matches Codex's file-editing tool calls.
const CodexEditTools = "apply_patch|Edit|Write"

func (codex) Parse(ev Event, payload []byte) Input { return claude{}.Parse(ev, payload) }

func (codex) Write(ev Event, out Output, stdout, _ io.Writer) int {
	return writeClaudeStyle(ev, out, stdout)
}
