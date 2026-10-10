package hooks

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestAdaptersParse(t *testing.T) {
	cases := []struct {
		agent, payload string
		want           Input
	}{
		{"claude", `{"cwd":"/r","tool_input":{"file_path":"/r/a.go"}}`, Input{Dir: "/r", Files: []string{"/r/a.go"}}},
		{"claude", `{"cwd":"/r","stop_hook_active":true}`, Input{Dir: "/r", StopActive: true}},
		{"codex", `{"cwd":"/r","tool_name":"apply_patch","tool_input":{"command":"*** Begin Patch\n*** Update File: src/x.rs\n@@\n*** Add File: src/y.rs\n+hi\n*** End Patch"}}`,
			Input{Dir: "/r", Files: []string{"src/x.rs", "src/y.rs"}}},
		{"cursor", `{"workspace_roots":["/w"],"tool_input":{"file_path":"/w/b.ts"}}`, Input{Dir: "/w", Files: []string{"/w/b.ts"}}},
		{"cursor", `{"workspace_roots":["/w"],"file_path":"/w/c.ts"}`, Input{Dir: "/w", Files: []string{"/w/c.ts"}}},
		{"cursor", `{"workspace_roots":["/w"],"loop_count":1}`, Input{Dir: "/w", StopActive: true}},
		{"opencode", `{"dir":"/o","files":["d.go"]}`, Input{Dir: "/o", Files: []string{"d.go"}}},
	}
	for _, c := range cases {
		got := Adapters[c.agent].Parse(PreEdit, []byte(c.payload))
		if got.Dir != c.want.Dir || strings.Join(got.Files, ",") != strings.Join(c.want.Files, ",") || got.StopActive != c.want.StopActive {
			t.Errorf("%s %s: got %+v want %+v", c.agent, c.payload, got, c.want)
		}
	}
}

func TestAdaptersWrite(t *testing.T) {
	block := Output{Block: true, Reason: "no ticket"}
	var out bytes.Buffer
	decode := func() map[string]any {
		var m map[string]any
		_ = json.Unmarshal(out.Bytes(), &m)
		out.Reset()
		return m
	}
	t.Setenv("CURSOR_VERSION", "")

	Adapters["claude"].Write(PreEdit, block, &out, &out)
	if hs := decode()["hookSpecificOutput"].(map[string]any); hs["permissionDecision"] != "deny" || hs["permissionDecisionReason"] != "no ticket" {
		t.Errorf("claude deny wrong: %v", hs)
	}
	Adapters["codex"].Write(Stop, block, &out, &out)
	if m := decode(); m["decision"] != "block" || m["reason"] != "no ticket" {
		t.Errorf("codex stop wrong: %v", m)
	}
	Adapters["claude"].Write(SessionStart, Output{Context: "ctx"}, &out, &out)
	if hs := decode()["hookSpecificOutput"].(map[string]any); hs["additionalContext"] != "ctx" {
		t.Errorf("claude context wrong: %v", hs)
	}
	Adapters["cursor"].Write(PreEdit, Output{}, &out, &out)
	if m := decode(); m["permission"] != "allow" {
		t.Errorf("cursor must always answer permission hooks with JSON: %v", m)
	}
	Adapters["cursor"].Write(PreEdit, block, &out, &out)
	if m := decode(); m["permission"] != "deny" || m["agent_message"] != "no ticket" {
		t.Errorf("cursor deny wrong: %v", m)
	}
	Adapters["cursor"].Write(Stop, block, &out, &out)
	if m := decode(); m["followup_message"] != "no ticket" {
		t.Errorf("cursor stop wrong: %v", m)
	}
	if code := Adapters["opencode"].Write(PreEdit, block, &out, &out); code != 2 || out.String() != "no ticket" {
		t.Errorf("opencode block: code %d out %q", code, out.String())
	}
	out.Reset()

	t.Setenv("CURSOR_VERSION", "3.14")
	Adapters["claude"].Write(PreEdit, block, &out, &out)
	if out.Len() != 0 {
		t.Error("inside Cursor the Claude-format hook must stay silent (Cursor runs its own)")
	}
}
