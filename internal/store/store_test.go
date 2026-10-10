package store

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/RL-Charles/sherpa/internal/model"
)

func TestRoundTrip(t *testing.T) {
	in := &model.Item{ID: "T-0001", Kind: model.Ticket, Title: "Hello: world", Status: model.Ready, Parent: "S-001",
		Labels: []string{"cli"}, Body: "## Description\n\n---\n\nhorizontal rule above\n"}
	data, err := Render(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if out.Title != in.Title || out.Parent != in.Parent || out.Status != in.Status || out.Body != in.Body {
		t.Fatalf("round trip mismatch:\n%+v\n%+v", in, out)
	}
}

func TestCreateHierarchy(t *testing.T) {
	s, err := Init(t.TempDir(), "SHP", "sherpa")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(NewItem{Kind: model.Ticket, Title: "orphan"}); err == nil {
		t.Fatal("ticket without parent should fail")
	}
	e, err := s.Create(NewItem{Kind: model.Epic, Title: "Epic"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(NewItem{Kind: model.Ticket, Title: "wrong parent", Parent: e.ID}); err == nil {
		t.Fatal("ticket under an epic should fail")
	}
	st, err := s.Create(NewItem{Kind: model.Story, Title: "Story", Parent: "e1"})
	if err != nil {
		t.Fatal(err)
	}
	t1, _ := s.Create(NewItem{Kind: model.Ticket, Title: "one", Parent: st.ID})
	t2, _ := s.Create(NewItem{Kind: model.Ticket, Title: "two", Parent: st.ID})
	if t1.ID != "T-0001" || t2.ID != "T-0002" || st.Parent != "E-001" {
		t.Fatalf("unexpected ids: %s %s parent=%s", t1.ID, t2.ID, st.Parent)
	}
	all, err := s.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 || all[0].Kind != model.Epic || len(Children(all, st.ID)) != 2 {
		t.Fatalf("unexpected listing: %d items", len(all))
	}
}

func TestStripWorklog(t *testing.T) {
	body := "## Description\n\nx\n\n## Worklog\n\n- a\n- b\n\n## Notes\n\nkeep\n"
	got, changed := StripWorklog(body)
	if !changed || got != "## Description\n\nx\n\n## Notes\n\nkeep\n" {
		t.Fatalf("got %q", got)
	}
	if _, changed := StripWorklog(got); changed {
		t.Fatal("second strip should be a no-op")
	}
	if got, _ := StripWorklog("## Goal\n\ng\n\n## Worklog\n- a\n"); got != "## Goal\n\ng\n" {
		t.Fatalf("trailing worklog: got %q", got)
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestFindSharesMainCheckoutFromWorktree(t *testing.T) {
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	if _, err := Init(repo, "WT", "wt"); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-qm", "init")
	wt := filepath.Join(repo, ".claude", "worktrees", "agent-1")
	gitRun(t, repo, "worktree", "add", "-q", "-b", "agent/x", wt)

	s, err := Find(filepath.Join(wt))
	if err != nil {
		t.Fatal(err)
	}
	mainDir, _ := filepath.EvalSymlinks(filepath.Join(repo, DirName))
	gotDir, _ := filepath.EvalSymlinks(s.Dir)
	gotCode, _ := filepath.EvalSymlinks(s.CodeRoot)
	wantCode, _ := filepath.EvalSymlinks(wt)
	if gotDir != mainDir {
		t.Fatalf("worktree should use main checkout's .sherpa: got %s want %s", gotDir, mainDir)
	}
	if gotCode != wantCode {
		t.Fatalf("CodeRoot should be the worktree: got %s want %s", gotCode, wantCode)
	}

	t.Setenv("SHERPA_DIR", filepath.Join(wt, DirName)) // explicit override wins
	s, err = Find(repo)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := filepath.EvalSymlinks(s.Dir); got != func() string { p, _ := filepath.EvalSymlinks(filepath.Join(wt, DirName)); return p }() {
		t.Fatalf("SHERPA_DIR ignored: %s", s.Dir)
	}
}
