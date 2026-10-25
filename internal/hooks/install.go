package hooks

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed templates/opencode-plugin.ts
var opencodePlugin string

// Agents that `shp hooks install` knows how to wire up.
var Agents = []string{"claude", "cursor", "codex", "opencode"}

// Result reports what happened to one integration point.
type Result struct {
	Target string // claude, cursor, codex, opencode, git, docs
	Path   string
	State  string // installed, updated, unchanged, removed, absent, skipped
	Note   string
}

const (
	ourMarker     = "shp hook "
	pluginMarker  = "Sherpa ticket tracking for OpenCode"
	gitHookMarker = "# Sherpa:"
)

// command builds a hook command that fails open: if shp is not installed the
// agent is never blocked. fallback is printed instead when a tool requires
// output (Cursor permission hooks must always answer with JSON).
func command(event Event, agent, fallback string) string {
	run := fmt.Sprintf("shp hook %s --agent %s", event, agent)
	if fallback == "" {
		return "command -v shp >/dev/null 2>&1 && " + run + " || true"
	}
	return fmt.Sprintf("if command -v shp >/dev/null 2>&1; then %s; else echo '%s'; fi", run, fallback)
}

// Install wires Sherpa into each agent's hook system under root (the
// checkout holding .sherpa), plus the git commit-msg hook and agent docs.
func Install(root string, agents []string, git, docs bool) []Result {
	var out []Result
	for _, a := range agents {
		switch a {
		case "claude":
			out = append(out, installJSON(root, a, filepath.Join(".claude", "settings.json"), claudeShaped(a, ClaudeEditTools, "startup|resume|clear|compact")))
		case "codex":
			r := installJSON(root, a, filepath.Join(".codex", "hooks.json"), claudeShaped(a, CodexEditTools, "startup|resume|clear|compact"))
			r.Note = "Codex runs project hooks only in trusted projects, after you approve them in /hooks"
			out = append(out, r)
		case "cursor":
			out = append(out, installJSON(root, a, filepath.Join(".cursor", "hooks.json"), cursorHooks()))
		case "opencode":
			out = append(out, installPlugin(root))
		default:
			out = append(out, Result{Target: a, State: "skipped", Note: "unknown agent (want " + strings.Join(Agents, ", ") + ")"})
		}
	}
	if git {
		out = append(out, installGitHook(root))
	}
	if docs {
		out = append(out, installDocs(root)...)
	}
	return out
}

// Uninstall removes everything Install added (agent docs are left alone).
func Uninstall(root string, agents []string, git bool) []Result {
	var out []Result
	for _, a := range agents {
		switch a {
		case "claude":
			out = append(out, uninstallJSON(root, a, filepath.Join(".claude", "settings.json")))
		case "codex":
			out = append(out, uninstallJSON(root, a, filepath.Join(".codex", "hooks.json")))
		case "cursor":
			out = append(out, uninstallJSON(root, a, filepath.Join(".cursor", "hooks.json")))
		case "opencode":
			p := filepath.Join(root, ".opencode", "plugins", "sherpa.ts")
			out = append(out, removeIfOurs(a, p, pluginMarker))
		}
	}
	if git {
		p, err := gitHookPath(root)
		if err == nil {
			out = append(out, removeIfOurs("git", p, gitHookMarker))
		}
	}
	return out
}

// Status reports which integrations are present.
func Status(root string) []Result {
	var out []Result
	for _, c := range []struct{ agent, rel string }{
		{"claude", ".claude/settings.json"}, {"cursor", ".cursor/hooks.json"}, {"codex", ".codex/hooks.json"},
	} {
		p := filepath.Join(root, c.rel)
		data, _ := os.ReadFile(p)
		state := "absent"
		if strings.Contains(string(data), ourMarker) {
			state = "installed"
		}
		out = append(out, Result{Target: c.agent, Path: c.rel, State: state})
	}
	plugin := filepath.Join(".opencode", "plugins", "sherpa.ts")
	data, _ := os.ReadFile(filepath.Join(root, plugin))
	out = append(out, Result{Target: "opencode", Path: plugin, State: map[bool]string{true: "installed", false: "absent"}[strings.Contains(string(data), pluginMarker)]})
	if p, err := gitHookPath(root); err == nil {
		data, _ := os.ReadFile(p)
		rel, _ := filepath.Rel(root, p)
		out = append(out, Result{Target: "git", Path: rel, State: map[bool]string{true: "installed", false: "absent"}[strings.Contains(string(data), gitHookMarker)]})
	}
	return out
}

// Agent hook settings --------------------------------------------------------

type hookSpec struct {
	event   string // the agent's event name
	matcher string
	entry   map[string]any // the agent-specific hook entry (command etc.)
}

// claudeShaped is the nested {matcher, hooks:[{type, command}]} layout used by
// Claude Code and Codex.
func claudeShaped(agent, editTools, startMatcher string) []hookSpec {
	cmd := func(ev Event) map[string]any {
		return map[string]any{"type": "command", "command": command(ev, agent, ""), "timeout": 30}
	}
	return []hookSpec{
		{"SessionStart", startMatcher, cmd(SessionStart)},
		{"PreToolUse", editTools, cmd(PreEdit)},
		{"PostToolUse", editTools, cmd(PostEdit)},
		{"PostToolUse", "Bash", cmd(PostShell)},
		{"Stop", "", cmd(Stop)},
		{"UserPromptSubmit", "", cmd(Working)},
		{"PreToolUse", "", cmd(Working)},
	}
}

func cursorHooks() []hookSpec {
	return []hookSpec{
		{"sessionStart", "", map[string]any{"command": command(SessionStart, "cursor", ""), "timeout": 30}},
		{"preToolUse", "Write|Delete", map[string]any{"command": command(PreEdit, "cursor", `{"permission":"allow"}`), "timeout": 30}},
		{"afterFileEdit", "", map[string]any{"command": command(PostEdit, "cursor", ""), "timeout": 30}},
		{"afterShellExecution", "", map[string]any{"command": command(PostShell, "cursor", ""), "timeout": 30}},
		{"stop", "", map[string]any{"command": command(Stop, "cursor", ""), "timeout": 30, "loop_limit": 1}},
		{"beforeSubmitPrompt", "", map[string]any{"command": command(Working, "cursor", `{"continue":true}`), "timeout": 30}},
	}
}

func readJSON(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	m := map[string]any{}
	if len(strings.TrimSpace(string(data))) == 0 {
		return m, nil
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", path, err)
	}
	return m, nil
}

func writeJSON(path string, m map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// ours reports whether a hook entry (flat or nested) runs `shp hook`.
func ours(entry any) bool {
	data, _ := json.Marshal(entry)
	return strings.Contains(string(data), ourMarker)
}

// stripOurs removes Sherpa entries from every event list, dropping empty lists.
func stripOurs(hooks map[string]any) {
	for ev, v := range hooks {
		list, ok := v.([]any)
		if !ok {
			continue
		}
		var kept []any
		for _, e := range list {
			if !ours(e) {
				kept = append(kept, e)
			}
		}
		if len(kept) == 0 {
			delete(hooks, ev)
		} else {
			hooks[ev] = kept
		}
	}
}

func installJSON(root, agent, rel string, specs []hookSpec) Result {
	path := filepath.Join(root, rel)
	res := Result{Target: agent, Path: rel}
	m, err := readJSON(path)
	if err != nil {
		res.State, res.Note = "skipped", err.Error()
		return res
	}
	before, _ := json.Marshal(m)
	hooks, _ := m["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	stripOurs(hooks)
	for _, sp := range specs {
		var entry map[string]any
		if agent == "cursor" { // flat: {command, matcher?, ...}
			entry = map[string]any{}
			for k, v := range sp.entry {
				entry[k] = v
			}
			if sp.matcher != "" {
				entry["matcher"] = sp.matcher
			}
		} else { // nested: {matcher?, hooks: [{type, command}]}
			entry = map[string]any{"hooks": []any{sp.entry}}
			if sp.matcher != "" {
				entry["matcher"] = sp.matcher
			}
		}
		list, _ := hooks[sp.event].([]any)
		hooks[sp.event] = append(list, entry)
	}
	m["hooks"] = hooks
	if agent == "cursor" {
		m["version"] = 1
	}
	after, _ := json.Marshal(m)
	switch {
	case string(before) == string(after):
		res.State = "unchanged"
		return res
	case strings.Contains(string(before), ourMarker):
		res.State = "updated"
	default:
		res.State = "installed"
	}
	if err := writeJSON(path, m); err != nil {
		res.State, res.Note = "skipped", err.Error()
	}
	return res
}

func uninstallJSON(root, agent, rel string) Result {
	path := filepath.Join(root, rel)
	res := Result{Target: agent, Path: rel, State: "absent"}
	m, err := readJSON(path)
	if err != nil || !strings.Contains(fmt.Sprint(m), ourMarker) {
		return res
	}
	if hooks, ok := m["hooks"].(map[string]any); ok {
		stripOurs(hooks)
		if len(hooks) == 0 {
			delete(m, "hooks")
		}
	}
	if agent == "cursor" && len(m) == 1 && m["version"] != nil {
		delete(m, "version")
	}
	if len(m) == 0 {
		_ = os.Remove(path)
	} else if err := writeJSON(path, m); err != nil {
		res.Note = err.Error()
		return res
	}
	res.State = "removed"
	return res
}

// OpenCode plugin and git hook -------------------------------------------------

func installPlugin(root string) Result {
	rel := filepath.Join(".opencode", "plugins", "sherpa.ts")
	return writeIfOurs("opencode", root, rel, opencodePlugin, pluginMarker, 0o644)
}

// writeIfOurs writes content unless a different, non-Sherpa file is there.
func writeIfOurs(target, root, rel, content, marker string, mode os.FileMode) Result {
	path := filepath.Join(root, rel)
	res := Result{Target: target, Path: rel}
	existing, err := os.ReadFile(path)
	switch {
	case err == nil && string(existing) == content:
		res.State = "unchanged"
		return res
	case err == nil && !strings.Contains(string(existing), marker):
		res.State, res.Note = "skipped", "a different file is already there; not overwriting it"
		return res
	case err == nil:
		res.State = "updated"
	default:
		res.State = "installed"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		res.State, res.Note = "skipped", err.Error()
		return res
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		res.State, res.Note = "skipped", err.Error()
	}
	return res
}

func removeIfOurs(target, path, marker string) Result {
	res := Result{Target: target, Path: path, State: "absent"}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), marker) {
		return res
	}
	if err := os.Remove(path); err != nil {
		res.Note = err.Error()
		return res
	}
	res.State = "removed"
	return res
}

const gitHook = `#!/bin/sh
# Sherpa: every commit names its ticket (installed by ` + "`shp hooks install`" + `).
command -v shp >/dev/null 2>&1 || exit 0
exec shp hook commit-msg "$1"
`

// gitHookPath honours core.hooksPath and worktrees (hooks are shared).
func gitHookPath(root string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--path-format=absolute", "--git-path", "hooks")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return filepath.Join(strings.TrimSpace(string(out)), "commit-msg"), nil
}

func installGitHook(root string) Result {
	p, err := gitHookPath(root)
	if err != nil {
		return Result{Target: "git", State: "skipped", Note: "not a git repository"}
	}
	rel, _ := filepath.Rel(root, p)
	res := writeIfOurs("git", root, rel, gitHook, gitHookMarker, 0o755)
	if res.State == "skipped" && strings.HasPrefix(res.Note, "a different") {
		res.Note = `a commit-msg hook already exists; add this line to it: shp hook commit-msg "$1"`
	}
	return res
}

// Agent docs -----------------------------------------------------------------

const protocolDoc = `
## Work tracking (Sherpa)

Work in this repo is tracked with Sherpa (` + "`shp`" + `, tickets in ` + "`.sherpa/`" + `). Hooks enforce it:
edits are blocked until a ticket is claimed, and stopping with unlogged changes asks for a log.

1. ` + "`shp context`" + ` to see what to work on; ` + "`shp brief <T-id>`" + ` for the compact outcome,
   deliverables, next check and code map; ` + "`shp show <T-id>`" + ` for the full spec and history.
2. ` + "`shp start <T-id>`" + ` before changing code. If nothing fits: ` + "`shp new ticket \"…\" -p <S-id> --template feature`" + `, then start it.
3. Pin the brief before the diff: ` + "`shp deliverable add \"outcome\"`" + `,
   ` + "`shp where add <file> --symbols <Type,Func> --deliverable N`" + `,
   ` + "`shp check add --for N \"unit: …\"`" + `, and ` + "`shp design <T-id> --stdin`" + ` for types/enums,
   patterns, invariants, contracts and edge cases.
4. Log as you go: ` + "`shp log \"headline\" -f <file>`" + `, with ` + "`-k decision|verify|blocker`" + ` and ` + "`-d`" + ` for details.
5. Keep the ticket readable: ` + "`shp now \"one-line state\"`" + `, tick acceptance checks with evidence
   (` + "`shp check <n> -e <sha|path|PR>`" + `), record measurables (` + "`shp measure \"<name>\" <value> --target <goal>`" + `).
   Anything you can't verify yourself (looks, feel, real devices) is a manual check for a human:
   ` + "`shp check add --manual \"…\" --how \"steps\"`" + `. Only humans tick those.
   New epics list their deliverables: ` + "`shp new epic \"…\" -g \"goal\" -g \"goal\"`" + ` (at least two).
   Stories and epics need a Visual: ` + "`shp visual <S-id> --stdin`" + ` (a generated one shows until then).
6. Run ` + "`shp lint <T-id>`" + ` before submitting; fix missing Why/Context/Design, deliverables without
   checks, ticked checks without evidence and missing code-map entries.
7. Open the PR yourself (` + "`gh pr create`" + `); Sherpa links it to the ticket. ` + "`shp sync`" + ` links PRs made elsewhere.
8. Finish: ` + "`shp submit -m \"what changed\" --verified \"how it was checked\"`" + `. It warns about anything missing and lands in agent review.
   Reviewing agent work when asked: ` + "`shp review <T-id> --pass -m \"what you checked\"`" + ` (on to human review) or ` + "`--reject -m \"what to fix\"`" + `.
   Only a human marks work done.
9. Never edit ` + "`.sherpa/`" + ` by hand. Commits get a ` + "`Ticket: T-xxxx`" + ` trailer automatically.
`

// protocolHeading lets installDocs replace an older Sherpa protocol instead of
// appending a second copy.
const protocolHeading = "## Work tracking (Sherpa)"

// upsertProtocol updates an existing Sherpa section in place, preserving any
// user content before and after it. It reports whether the file changed.
func upsertProtocol(data string) (string, bool) {
	if strings.Contains(data, "shp brief") && strings.Contains(data, "shp where add") {
		return data, false
	}
	body := strings.TrimRight(data, "\n")
	if i := strings.Index(body, protocolHeading); i >= 0 {
		end := len(body)
		if j := strings.Index(body[i+len(protocolHeading):], "\n## "); j >= 0 {
			end = i + len(protocolHeading) + j + 1
		}
		return strings.TrimRight(body[:i], "\n") + "\n\n" + strings.TrimSpace(protocolDoc) + "\n\n" + strings.TrimLeft(body[end:], "\n"), true
	}
	return strings.TrimRight(body, "\n") + "\n" + protocolDoc, true
}

// installDocs makes sure agents read the protocol: AGENTS.md (read by Codex,
// Cursor and OpenCode) and CLAUDE.md, which imports AGENTS.md.
func installDocs(root string) []Result {
	var out []Result
	agents := filepath.Join(root, "AGENTS.md")
	data, err := os.ReadFile(agents)
	switch {
	case err == nil:
		if next, changed := upsertProtocol(string(data)); changed {
			if err := os.WriteFile(agents, []byte(next), 0o644); err == nil {
				out = append(out, Result{Target: "docs", Path: "AGENTS.md", State: "updated"})
			}
		} else {
			out = append(out, Result{Target: "docs", Path: "AGENTS.md", State: "unchanged"})
		}
	default:
		if err := os.WriteFile(agents, []byte("# Agent instructions\n"+protocolDoc), 0o644); err == nil {
			out = append(out, Result{Target: "docs", Path: "AGENTS.md", State: "installed"})
		}
	}
	claudeMD := filepath.Join(root, "CLAUDE.md")
	data, err = os.ReadFile(claudeMD)
	switch {
	case err == nil && (strings.Contains(string(data), "@AGENTS.md") || strings.Contains(string(data), "shp start")):
		out = append(out, Result{Target: "docs", Path: "CLAUDE.md", State: "unchanged"})
	case err == nil:
		if err := os.WriteFile(claudeMD, []byte(strings.TrimRight(string(data), "\n")+"\n\n@AGENTS.md\n"), 0o644); err == nil {
			out = append(out, Result{Target: "docs", Path: "CLAUDE.md", State: "updated"})
		}
	default:
		if err := os.WriteFile(claudeMD, []byte("@AGENTS.md\n"), 0o644); err == nil {
			out = append(out, Result{Target: "docs", Path: "CLAUDE.md", State: "installed"})
		}
	}
	return out
}
