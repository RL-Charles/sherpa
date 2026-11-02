package hooks

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain keeps the global OpenCode plugin out of the real config directory.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "sherpa-xdg-")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_CONFIG_HOME", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func TestOpencodePluginIsGlobalAndReplacesPerRepoCopy(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	global, _ := opencodeGlobalPlugin()
	root := gitInit(t)
	legacy := filepath.Join(root, legacyPlugin)
	_ = os.MkdirAll(filepath.Dir(legacy), 0o755)
	_ = os.WriteFile(legacy, []byte("// "+pluginMarker+" (V1)\n"), 0o644)
	if st := Status(root); st[3].Note == "" {
		t.Errorf("status should note the per-repo copy: %+v", st[3])
	}

	rs := Install(root, []string{"opencode"}, false, false)
	if len(rs) != 2 || rs[0].State != "installed" || rs[1].State != "removed" {
		t.Fatalf("want global installed and per-repo removed: %+v", rs)
	}
	if data, _ := os.ReadFile(global); string(data) != opencodePlugin {
		t.Fatalf("global plugin not written to %s", global)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("per-repo copy should be gone: %v", err)
	}
	if st := Status(root)[3]; st.State != "installed" || st.Note != "" {
		t.Errorf("status after install: %+v", st)
	}
	// A per-repo file that isn't Sherpa's is left alone.
	_ = os.WriteFile(legacy, []byte("// someone else's plugin\n"), 0o644)
	if rs := Install(root, []string{"opencode"}, false, false); len(rs) != 1 || rs[0].State != "unchanged" {
		t.Fatalf("second install: %+v", rs)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("foreign plugin must be kept: %v", err)
	}
	rs = Uninstall(root, []string{"opencode"}, false)
	if _, err := os.Stat(global); !os.IsNotExist(err) || rs[1].State != "removed" {
		t.Fatalf("uninstall should remove the global plugin: %+v", rs)
	}
}

func gitInit(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	return root
}

func states(rs []Result) map[string]string {
	m := map[string]string{}
	for _, r := range rs {
		m[r.Target+":"+filepath.Base(r.Path)] = r.State
	}
	return m
}

// OpenCode V2 loads only a default export with an id and setup; V1 plugin
// functions fail to load, and its tools are shell, edit, write and patch.
func TestOpencodePluginTargetsV2(t *testing.T) {
	for _, want := range []string{"export default {", `id: "sherpa"`, "async setup(ctx", `"shell"`, `"patch"`, "input?.path", pluginMarker} {
		if !strings.Contains(opencodePlugin, want) {
			t.Errorf("plugin template missing %q", want)
		}
	}
	for _, v1 := range []string{"@opencode-ai/plugin", "apply_patch", "filePath", `"bash"`, "promptAsync"} {
		if strings.Contains(opencodePlugin, v1) {
			t.Errorf("plugin template still uses V1 %q", v1)
		}
	}
}

func TestProtocolUpgradePreservesOtherSections(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "AGENTS.md")
	old := "# Agents\n\n## Work tracking (Sherpa)\n\n1. shp start <T-id> before editing.\n\n## House rules\n\n- keep this\n"
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	first := states(Install(root, nil, false, true))
	if first["docs:AGENTS.md"] != "updated" {
		t.Fatalf("old protocol should update: %v", first)
	}
	data, _ := os.ReadFile(path)
	for _, want := range []string{"shp brief", "shp where add", "## House rules", "- keep this"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("upgraded AGENTS.md missing %q:\n%s", want, data)
		}
	}
	if strings.Count(string(data), protocolHeading) != 1 {
		t.Errorf("protocol heading should appear once:\n%s", data)
	}
	second := states(Install(root, nil, false, true))
	if second["docs:AGENTS.md"] != "unchanged" {
		t.Fatalf("second install should be idempotent: %v", second)
	}
}

func TestInstallPreservesAndIsIdempotent(t *testing.T) {
	root := gitInit(t)
	existing := `{"model":"opus","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"my-guard.sh"}]}]}}`
	_ = os.MkdirAll(filepath.Join(root, ".claude"), 0o755)
	_ = os.WriteFile(filepath.Join(root, ".claude", "settings.json"), []byte(existing), 0o644)

	first := states(Install(root, Agents, true, true))
	for k, want := range map[string]string{"claude:settings.json": "installed", "cursor:hooks.json": "installed", "codex:hooks.json": "installed",
		"opencode:sherpa.ts": "installed", "git:commit-msg": "installed", "docs:AGENTS.md": "installed", "docs:CLAUDE.md": "installed"} {
		if first[k] != want {
			t.Errorf("%s: %s, want %s", k, first[k], want)
		}
	}
	for k, st := range states(Install(root, Agents, true, true)) {
		if st != "unchanged" {
			t.Errorf("second install should change nothing: %s %s", k, st)
		}
	}

	var m map[string]any
	data, _ := os.ReadFile(filepath.Join(root, ".claude", "settings.json"))
	_ = json.Unmarshal(data, &m)
	pre := m["hooks"].(map[string]any)["PreToolUse"].([]any)
	if m["model"] != "opus" || len(pre) != 3 || // theirs, pre-edit, working
		!strings.Contains(string(data), "my-guard.sh") {
		t.Fatalf("existing settings and hooks must be kept:\n%s", data)
	}
	if !strings.Contains(string(data), "command -v shp") || !strings.Contains(string(data), "Edit|Write|MultiEdit|NotebookEdit") {
		t.Fatalf("expected fail-open edit hooks:\n%s", data)
	}
	cursor, _ := os.ReadFile(filepath.Join(root, ".cursor", "hooks.json"))
	if !strings.Contains(string(cursor), `"version": 1`) || !strings.Contains(string(cursor), `{\"permission\":\"allow\"}`) {
		t.Fatalf("cursor hooks need version 1 and a JSON fallback:\n%s", cursor)
	}
	if fi, err := os.Stat(filepath.Join(root, ".git", "hooks", "commit-msg")); err != nil || fi.Mode()&0o111 == 0 {
		t.Fatal("git hook should be installed and executable")
	}

	gone := states(Uninstall(root, Agents, true))
	for k, st := range gone {
		if st != "removed" {
			t.Errorf("uninstall %s: %s", k, st)
		}
	}
	data, _ = os.ReadFile(filepath.Join(root, ".claude", "settings.json"))
	if strings.Contains(string(data), ourMarker) || !strings.Contains(string(data), "my-guard.sh") {
		t.Fatalf("uninstall must remove only Sherpa's hooks:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(root, ".cursor", "hooks.json")); !os.IsNotExist(err) {
		t.Error("a hooks file Sherpa created and emptied should be removed")
	}
}

func TestInstallLeavesForeignGitHook(t *testing.T) {
	root := gitInit(t)
	p := filepath.Join(root, ".git", "hooks", "commit-msg")
	_ = os.WriteFile(p, []byte("#!/bin/sh\nmy-linter \"$1\"\n"), 0o755)
	r := Install(root, nil, true, false)
	if len(r) != 1 || r[0].State != "skipped" || !strings.Contains(r[0].Note, `shp hook commit-msg "$1"`) {
		t.Fatalf("existing hook must not be overwritten: %+v", r)
	}
	if data, _ := os.ReadFile(p); !strings.Contains(string(data), "my-linter") {
		t.Fatal("foreign hook was modified")
	}
}
