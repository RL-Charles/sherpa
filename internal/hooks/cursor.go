package hooks

import (
	"encoding/json"
	"io"
)

// cursor adapts Cursor agent hooks (.cursor/hooks.json), IDE and CLI.
// https://cursor.com/docs/agent/hooks
//
//	sessionStart  → additional_context (Cursor may drop it; AGENTS.md backs it up)
//	preToolUse    → deny Write/Delete with agent_message
//	afterFileEdit → record the file
//	stop          → followup_message keeps the agent going once
//	beforeSubmitPrompt → working heartbeat (always continue)
type cursor struct{}

func init() { Adapters["cursor"] = cursor{} }

func (cursor) Parse(ev Event, payload []byte) Input {
	var p struct {
		Cwd            string   `json:"cwd"`
		WorkspaceRoots []string `json:"workspace_roots"`
		FilePath       string   `json:"file_path"`
		LoopCount      int      `json:"loop_count"`
		Command        string   `json:"command"`
		ToolInput      struct {
			FilePath string `json:"file_path"`
			Path     string `json:"path"`
		} `json:"tool_input"`
	}
	_ = json.Unmarshal(payload, &p)
	in := Input{Dir: p.Cwd, StopActive: p.LoopCount > 0}
	if in.Dir == "" && len(p.WorkspaceRoots) > 0 {
		in.Dir = p.WorkspaceRoots[0]
	}
	if ev == PostShell {
		in.Command, in.Output = p.Command, string(payload)
		return in
	}
	for _, f := range []string{p.FilePath, p.ToolInput.FilePath, p.ToolInput.Path} {
		if f != "" {
			in.Files = append(in.Files, f)
		}
	}
	return in
}

func (cursor) Write(ev Event, out Output, stdout, _ io.Writer) int {
	var resp any
	switch ev {
	case SessionStart:
		if out.Context != "" {
			resp = map[string]any{"additional_context": out.Context}
		}
	case PreEdit:
		// Permission hooks must always print valid JSON.
		resp = map[string]any{"permission": "allow"}
		if out.Block {
			// Cursor may surface either field to the model; both carry the fix.
			resp = map[string]any{"permission": "deny", "agent_message": out.Reason, "user_message": out.Reason}
		}
	case Stop:
		if out.Block {
			resp = map[string]any{"followup_message": out.Reason}
		}
	case Working:
		resp = map[string]any{"continue": true}
	}
	if resp != nil {
		_ = json.NewEncoder(stdout).Encode(resp)
	}
	return 0
}
