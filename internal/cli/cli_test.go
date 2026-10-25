package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RL-Charles/sherpa/internal/model"
	"github.com/RL-Charles/sherpa/internal/store"
)

// project makes a store with E-001 › S-001 › T-0001 and runs commands in it.
func project(t *testing.T) *store.Store {
	t.Helper()
	root := t.TempDir()
	s, err := store.Init(root, "CL", "cli")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []store.NewItem{
		{Kind: model.Epic, Title: "E"},
		{Kind: model.Story, Title: "S", Parent: "E-001"},
		{Kind: model.Ticket, Title: "Do the thing", Parent: "S-001"},
	} {
		if _, err := s.Create(n); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(root)
	t.Setenv("SHERPA_SESSION", "test")
	return s
}

// shp runs one command as actor ("agent:claude" or "human:ann").
func shp(t *testing.T, actor string, args ...string) error {
	t.Helper()
	t.Setenv("SHERPA_ACTOR", actor)
	root := newRoot(&app{})
	root.SetArgs(args)
	return root.Execute()
}

func get(t *testing.T, s *store.Store, id string) *model.Item {
	t.Helper()
	it, err := s.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return it
}

func TestManualChecks(t *testing.T) {
	s := project(t)
	if err := shp(t, "agent:claude", "check", "add", "-t", "T-0001", "tests pass"); err != nil {
		t.Fatal(err)
	}
	if err := shp(t, "agent:claude", "check", "add", "-t", "T-0001", "--how", "resize to 80 cols", "looks right"); err != nil {
		t.Fatal(err)
	}
	it := get(t, s, "T-0001")
	if c := it.Checks[1]; !c.Manual || c.How != "resize to 80 cols" {
		t.Fatalf("--how should add a manual check with steps: %+v", c)
	}
	if got := it.OpenManual(); len(got) != 1 || got[0] != 2 {
		t.Fatalf("OpenManual = %v, want [2]", got)
	}

	if err := shp(t, "agent:claude", "check", "1", "-t", "T-0001", "-e", "go test"); err != nil {
		t.Fatalf("agents tick ordinary checks: %v", err)
	}
	err := shp(t, "agent:claude", "check", "2", "-t", "T-0001", "-e", "looked")
	if err == nil || !strings.Contains(err.Error(), "manual") {
		t.Fatalf("an agent must not tick a manual check, got %v", err)
	}
	if err := shp(t, "agent:claude", "check", "2", "-t", "T-0001", "--waive", "meh"); err == nil {
		t.Fatal("an agent must not waive a manual check")
	}
	if err := shp(t, "human:ann", "check", "2", "-t", "T-0001", "-e", "checked in tmux"); err != nil {
		t.Fatalf("a human ticks manual checks: %v", err)
	}
	if it := get(t, s, "T-0001"); len(it.OpenManual()) != 0 {
		t.Fatal("ticked manual check should no longer be open")
	}

	if err := shp(t, "agent:claude", "check", "1", "-t", "T-0001", "--manual", "--how", "eyeball it"); err != nil {
		t.Fatal(err)
	}
	if c := get(t, s, "T-0001").Checks[0]; !c.Manual || c.How != "eyeball it" || !c.Done {
		t.Fatalf("--manual marks an existing check without changing its tick: %+v", c)
	}
}

func TestNewManualCheck(t *testing.T) {
	s := project(t)
	if err := shp(t, "agent:claude", "new", "ticket", "Two", "-p", "S-001", "-c", "auto", "--manual-check", "by hand"); err != nil {
		t.Fatal(err)
	}
	it := get(t, s, "T-0002")
	if len(it.Checks) != 2 || it.Checks[0].Manual || !it.Checks[1].Manual {
		t.Fatalf("checks = %+v", it.Checks)
	}
}

func TestReviewWarningsSeparateManual(t *testing.T) {
	it := &model.Item{Checks: []model.Check{{Text: "a"}, {Text: "b", Manual: true}}, Now: "x"}
	w := strings.Join(reviewWarnings(it), "\n")
	if !strings.Contains(w, "unresolved 1 a") || strings.Contains(w, "unresolved 1 a; 2 b") {
		t.Fatalf("ordinary open checks warn on their own:\n%s", w)
	}
	if !strings.Contains(w, "✋ for a human to verify: 2 b") {
		t.Fatalf("manual checks are listed for the human:\n%s", w)
	}
}

// configure sets the project's reviewer in config.yaml.
func configure(t *testing.T, s *store.Store, reviewer string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(s.Dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte(reviewer)...)
	if err := os.WriteFile(filepath.Join(s.Dir, "config.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func submitAsAgent(t *testing.T) {
	t.Helper()
	if err := shp(t, "agent:claude", "start", "T-0001"); err != nil {
		t.Fatal(err)
	}
	if err := shp(t, "agent:claude", "submit", "-m", "did it", "--verified", "tests"); err != nil {
		t.Fatal(err)
	}
}

func TestNoSelfReview(t *testing.T) {
	s := project(t)
	submitAsAgent(t)
	err := shp(t, "agent:claude", "review", "T-0001", "--pass", "-m", "lgtm")
	if err == nil || !strings.Contains(err.Error(), "--spawn") {
		t.Fatalf("the submitting session must not review its own work, got %v", err)
	}
	t.Setenv("SHERPA_SESSION", "other")
	if err := shp(t, "agent:claude", "review", "T-0001", "--pass", "-m", "lgtm"); err != nil {
		t.Fatalf("another session may review: %v", err)
	}
	if it := get(t, s, "T-0001"); it.Status != model.Review {
		t.Fatalf("status = %s, want review", it.Status)
	}
}

func TestSubmitLaunchesReviewer(t *testing.T) {
	s := project(t)
	marker := filepath.Join(s.Root, "reviewed")
	configure(t, s, "reviewer:\n  command: cat > "+marker+"\n  auto: true\n")
	submitAsAgent(t)
	for range 100 {
		if data, _ := os.ReadFile(marker); strings.Contains(string(data), "T-0001") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("an agent submit should launch the configured reviewer")
}

func TestPolicyClosesOnPass(t *testing.T) {
	s := project(t)
	if err := os.WriteFile(filepath.Join(s.Dir, "policy.yaml"), []byte("auto_done:\n  - name: low-risk\n    under: [S-001]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := shp(t, "agent:claude", "deliverable", "add", "-t", "T-0001", "outcome"); err != nil {
		t.Fatal(err)
	}
	if err := shp(t, "agent:claude", "deliverable", "done", "1", "-t", "T-0001", "-e", "tests"); err != nil {
		t.Fatal(err)
	}
	if err := shp(t, "agent:claude", "check", "add", "-t", "T-0001", "--for", "1", "--manual", "eyeball"); err != nil {
		t.Fatal(err)
	}
	submitAsAgent(t)
	t.Setenv("SHERPA_SESSION", "reviewer")
	if err := shp(t, "agent:reviewer", "review", "T-0001", "--pass", "-m", "ok"); err != nil {
		t.Fatal(err)
	}
	if it := get(t, s, "T-0001"); it.Status != model.Review {
		t.Fatalf("an open manual check must keep it for a human, got %s", it.Status)
	}

	t.Setenv("SHERPA_SESSION", "test")
	if err := shp(t, "human:ann", "check", "1", "-t", "T-0001", "-e", "looked"); err != nil {
		t.Fatal(err)
	}
	if err := shp(t, "human:ann", "status", "T-0001", "in_progress"); err != nil {
		t.Fatal(err)
	}
	submitAsAgent(t)
	t.Setenv("SHERPA_SESSION", "reviewer")
	if err := shp(t, "agent:reviewer", "review", "T-0001", "--pass", "-m", "ok"); err != nil {
		t.Fatal(err)
	}
	if it := get(t, s, "T-0001"); it.Status != model.Done {
		t.Fatalf("policy should close it, got %s", it.Status)
	}
}

func TestNewEpicNeedsGoals(t *testing.T) {
	s := project(t)
	if err := shp(t, "agent:claude", "new", "epic", "Vague"); err == nil || !strings.Contains(err.Error(), "-g") {
		t.Fatalf("epic without goals: want a -g hint, got %v", err)
	}
	if err := shp(t, "agent:claude", "new", "epic", "Vague", "-g", "only one"); err == nil {
		t.Fatal("epic with one goal should be refused")
	}
	if err := shp(t, "agent:claude", "new", "epic", "Clear", "-m", "Why it matters.", "-g", "Ship the CLI", "-g", "Ship the TUI"); err != nil {
		t.Fatal(err)
	}
	it, err := s.Get("E-002")
	if err != nil {
		t.Fatal(err)
	}
	if got := model.Goals(it.Body); len(got) != 2 || got[1] != "Ship the TUI" {
		t.Fatalf("goals = %q in body %q", got, it.Body)
	}
	if err := shp(t, "agent:claude", "new", "ticket", "T", "-p", "S-001", "-g", "x"); err == nil {
		t.Fatal("--goal on a ticket should be refused")
	}
}
