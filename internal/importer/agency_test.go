package importer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RL-Charles/sherpa/internal/journal"
	"github.com/RL-Charles/sherpa/internal/model"
	"github.com/RL-Charles/sherpa/internal/store"
	"github.com/RL-Charles/sherpa/internal/worklog"
)

const issueDone = `---
key: APP-1
uid: x
status: done
priority: 0
created: 2026-10-01T05:58:10Z
updated: 2026-10-01T06:57:07Z
---
# Pin the config format

## Goal

Pin it. See ![](assets/APP-1-shot.png).

## Dependencies

None.

## Codex · 2026-10-01T06:00:00Z

Started the **parser**. Details follow.

## Codex · 2026-10-01T06:30:00Z decision recorded

Use YAML for every config file.

## Codex · 2026-10-01 physical checkpoint

Booted twice.

## Codex · 2026-10-01T06:57:07Z

Completed and merged [PR #5](https://github.com/x/y/pull/5) at ` + "`abc1234`" + `. make check passed.
`

func issue(key, status string, deps string) string {
	return "---\nkey: " + key + "\nstatus: " + status + "\npriority: 2\ncreated: 2026-10-02T00:00:00Z\nupdated: 2026-10-02T01:00:00Z\n---\n# " + key + " title\n\n## Dependencies\n\n" + deps + "\n"
}

func setup(t *testing.T) (*store.Store, string) {
	t.Helper()
	root := t.TempDir()
	src := filepath.Join(root, ".agency", "issues")
	if err := os.MkdirAll(filepath.Join(src, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"APP-1.md":              issueDone,
		"APP-2.md":              issue("APP-2", "in_progress", "APP-1") + "\n## Claude · 2026-10-02T02:00:00Z\n\nWorking on it.\n",
		"APP-3.md":              issue("APP-3", "todo", "APP-1"),
		"APP-4.md":              issue("APP-4", "todo", "APP-2, APP-3"),
		"README.md":             "# Issues\n",
		"assets/APP-1-shot.png": "png",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(src, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s, err := store.Init(root, "APP", "test")
	if err != nil {
		t.Fatal(err)
	}
	return s, src
}

func mapping(issues ...[]int) *Mapping {
	m := &Mapping{Key: "APP"}
	m.Epics = append(m.Epics, struct {
		Title   string `yaml:"title"`
		Goal    string `yaml:"goal"`
		Stories []struct {
			Title       string `yaml:"title"`
			Description string `yaml:"description"`
			Issues      []int  `yaml:"issues"`
		} `yaml:"stories"`
	}{Title: "Track H", Goal: "Hardware"})
	for i, is := range issues {
		m.Epics[0].Stories = append(m.Epics[0].Stories, struct {
			Title       string `yaml:"title"`
			Description string `yaml:"description"`
			Issues      []int  `yaml:"issues"`
		}{Title: "H" + string(rune('0'+i)), Issues: is})
	}
	return m
}

func TestImportAgency(t *testing.T) {
	s, src := setup(t)
	res, err := Agency(s, src, mapping([]int{1, 2}, []int{3, 4}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Epics != 1 || res.Stories != 2 || res.Tickets != 4 {
		t.Fatalf("unexpected counts: %+v", res)
	}
	want := map[string]model.Status{"T-0001": model.Done, "T-0002": model.InProgress, "T-0003": model.Ready, "T-0004": model.Backlog,
		"S-001": model.InProgress, "S-002": model.Ready, "E-001": model.InProgress}
	for id, st := range want {
		it, err := s.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if it.Status != st {
			t.Errorf("%s: status %s, want %s", id, it.Status, st)
		}
	}
	t1, _ := s.Get("T-0001")
	if t1.Title != "Pin the config format" || t1.Priority != "p0" || t1.Labels[0] != "app-1" || t1.Parent != "S-001" {
		t.Errorf("ticket fields wrong: %+v", t1)
	}
	if strings.Contains(t1.Body, "Codex ·") || !strings.Contains(t1.Body, "](../imports/agency/assets/APP-1-shot.png)") {
		t.Errorf("body should exclude comments and keep attachment links working:\n%s", t1.Body)
	}
	if t1.Updated.Format("15:04") != "06:57" {
		t.Errorf("import should keep original timestamps, got %s", t1.Updated)
	}
	if t2, _ := s.Get("T-0002"); t2.Assignee != "claude" {
		t.Errorf("in-progress assignee should be the last commenter, got %q", t2.Assignee)
	}
	if next, _ := s.NextID(model.Ticket); next != "T-0005" {
		t.Errorf("new tickets should continue after the last issue number, got %s", next)
	}

	events, _ := journal.ReadAll(s.Dir)
	ss := worklog.Build(journal.ForItem(events, "T-0001"))
	if len(ss) != 2 || ss[1].Open || ss[1].Actor != "agent:codex" {
		t.Fatalf("done issue should give planning + one closed codex session: %+v", ss)
	}
	var heads []string
	for _, e := range ss[1].Entries {
		heads = append(heads, e.Message+" | "+strings.Join(e.Detail, ""))
	}
	got := strings.Join(heads, "\n")
	for _, w := range []string{"Started the parser.", "Decision recorded: Use YAML for every config file.", "Completed and merged PR #5 at abc1234. | PR #5 · abc1234", "imported from Agency APP-1"} {
		if !strings.Contains(got, w) {
			t.Errorf("worklog missing %q in:\n%s", w, got)
		}
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "imports", "agency", "assets", "APP-1-shot.png")); err != nil {
		t.Error("attachments should be archived")
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "imports", "agency", "APP-1.md")); err != nil {
		t.Error("original issue files should be archived")
	}
}

func TestImportRejectsBadMapping(t *testing.T) {
	s, src := setup(t)
	_, err := Agency(s, src, mapping([]int{1, 2, 2}, []int{9}))
	if err == nil {
		t.Fatal("expected mapping errors")
	}
	for _, w := range []string{"issue 2 is in both", "issue 9, which does not exist", "not placed in any story: APP-3, APP-4"} {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error missing %q:\n%v", w, err)
		}
	}
	if items, _ := s.All(); len(items) != 0 {
		t.Error("a rejected mapping must not write anything")
	}
}

func TestImportNeedsEmptyProject(t *testing.T) {
	s, src := setup(t)
	if _, err := s.Create(store.NewItem{Kind: model.Epic, Title: "already here"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Agency(s, src, mapping([]int{1, 2, 3, 4})); err == nil || !strings.Contains(err.Error(), "empty project") {
		t.Fatalf("want empty-project error, got %v", err)
	}
}

func TestCommentLabelsAndKinds(t *testing.T) {
	is, err := ParseIssue([]byte(issueDone))
	if err != nil {
		t.Fatal(err)
	}
	if len(is.Comments) != 4 || is.Comments[2].Label != "physical checkpoint" || !is.Comments[2].TS.Equal(is.Comments[1].TS) || is.Comments[1].Label != "decision recorded" || is.Comments[0].Label != "" {
		t.Fatalf("labels not parsed: %+v", is.Comments)
	}
	if commentKind("decision recorded x") != journal.KindDecision || commentKind("core-0 branches qualified") != journal.KindVerify ||
		commentKind("F2 blocker: cannot build") != journal.KindBlocker || commentKind("started") != journal.KindProgress {
		t.Fatal("kind heuristics wrong")
	}
}
