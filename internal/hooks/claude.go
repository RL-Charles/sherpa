package hooks

import (
	"encoding/json"
	"io"
	"os"
	"regexp"
	"strings"
)

// claude adapts Claude Code hooks (.claude/settings.json).
// https://code.claude.com/docs/en/hooks
type claude struct{}

func init() { Adapters["claude"] = claude{} }

// ClaudeEditTools are the Claude Code tools that change files.
const ClaudeEditTools = "Edit|Write|MultiEdit|NotebookEdit"

func (claude) Parse(ev Event, payload []byte) Input {
	var p struct {
		Cwd            string `json:"cwd"`
		StopHookActive bool   `json:"stop_hook_active"`
		ToolInput      struct {
			FilePath     string `json:"file_path"`
			NotebookPath string `json:"notebook_path"`
			Command      string `json:"command"` // Codex apply_patch carries the patch here
		} `json:"tool_input"`
	}
	_ = json.Unmarshal(payload, &p)
	in := Input{Dir: p.Cwd, StopActive: p.StopHookActive}
	if ev == PostShell {
		// The shell's output field differs between agents and versions; the
		// PR URL is found anywhere in the payload.
		return Input{Dir: p.Cwd, Command: p.ToolInput.Command, Output: string(payload)}
	}
	for _, f := range []string{p.ToolInput.FilePath, p.ToolInput.NotebookPath} {
		if f != "" {
			in.Files = append(in.Files, f)
		}
	}
	in.Files = append(in.Files, PatchFiles(p.ToolInput.Command)...)
	return in
}

var rePatchFile = regexp.MustCompile(`(?m)^\*\*\* (?:Add|Update|Delete) File: (.+)$`)

// PatchFiles lists the files an apply_patch patch touches.
func PatchFiles(patch string) []string {
	var out []string
	for _, m := range rePatchFile.FindAllStringSubmatch(patch, -1) {
		out = append(out, strings.TrimSpace(m[1]))
	}
	return out
}

func (claude) Write(ev Event, out Output, stdout, _ io.Writer) int {
	// Cursor also runs .claude/settings.json hooks; its own adapter handles it.
	if os.Getenv("CURSOR_VERSION") != "" {
		return 0
	}
	return writeClaudeStyle(ev, out, stdout)
}

// writeClaudeStyle answers in the JSON shape Claude Code and Codex share.
func writeClaudeStyle(ev Event, out Output, stdout io.Writer) int {
	var resp any
	switch {
	case ev == SessionStart && out.Context != "":
		resp = map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName": "SessionStart", "additionalContext": out.Context}}
	case ev == PreEdit && out.Block:
		resp = map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": out.Reason}}
	case ev == Stop && out.Block:
		resp = map[string]any{"decision": "block", "reason": out.Reason}
	}
	if resp != nil {
		_ = json.NewEncoder(stdout).Encode(resp)
	}
	return 0
}
