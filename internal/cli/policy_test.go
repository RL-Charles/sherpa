package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RL-Charles/sherpa/internal/model"
	"github.com/RL-Charles/sherpa/internal/policy"
	"github.com/RL-Charles/sherpa/internal/store"
)

func writePolicy(t *testing.T, s *store.Store, yaml string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(s.Dir, policy.File), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
}

// readyTicket gives T-0001 a resolved deliverable and check.
func readyTicket(t *testing.T) {
	t.Helper()
	mustShp(t, "agent:claude", "deliverable", "add", "-t", "T-0001", "outcome")
	mustShp(t, "agent:claude", "deliverable", "done", "1", "-t", "T-0001", "-e", "tests")
	mustShp(t, "agent:claude", "check", "add", "-t", "T-0001", "--for", "1", "unit")
	mustShp(t, "agent:claude", "check", "1", "-t", "T-0001", "-e", "abc1234")
}

func reviewAs(t *testing.T, verdict string) {
	t.Helper()
	t.Setenv("SHERPA_SESSION", "reviewer")
	mustShp(t, "agent:reviewer", "review", "T-0001", verdict, "-m", "checked")
	t.Setenv("SHERPA_SESSION", "test")
}

func TestPolicyClosesOnPassByDefault(t *testing.T) {
	s := project(t)
	readyTicket(t)
	mustShp(t, "agent:claude", "check", "add", "-t", "T-0001", "--for", "1", "--manual", "eyeball")
	submitAsAgent(t)
	reviewAs(t, "--pass")
	if it := get(t, s, "T-0001"); it.Status != model.Review {
		t.Fatalf("an open manual check must keep it for a human, got %s", it.Status)
	}

	mustShp(t, "human:ann", "check", "2", "-t", "T-0001", "-e", "looked")
	mustShp(t, "human:ann", "status", "T-0001", "in_progress")
	submitAsAgent(t)
	reviewAs(t, "--pass")
	if it := get(t, s, "T-0001"); it.Status != model.Done {
		t.Fatalf("with no policy file a passing review should close it, got %s", it.Status)
	}
}

func TestPolicyHumanRuleHoldsTicket(t *testing.T) {
	s := project(t)
	writePolicy(t, s, "human_review:\n  - name: flagged\n    labels: [security]\n")
	it := get(t, s, "T-0001")
	it.Labels = []string{"security"}
	if err := s.Save(it); err != nil {
		t.Fatal(err)
	}
	readyTicket(t)
	submitAsAgent(t)
	err := shp(t, "agent:claude", "done", "T-0001")
	if err == nil || !strings.Contains(err.Error(), "rule flagged") {
		t.Fatalf("an agent must not close a ticket a human_review rule matches: %v", err)
	}
	reviewAs(t, "--pass")
	if it := get(t, s, "T-0001"); it.Status != model.Review {
		t.Fatalf("a matching rule should send it to a human, got %s", it.Status)
	}
	mustShp(t, "human:ann", "done", "T-0001")
	if it := get(t, s, "T-0001"); it.Status != model.Done {
		t.Fatalf("a human closes it, got %s", it.Status)
	}
}

func TestPolicyStoriesStayHumanOnly(t *testing.T) {
	s := project(t)
	err := shp(t, "agent:claude", "done", "S-001")
	if err == nil || !strings.Contains(err.Error(), "only a human") {
		t.Fatalf("stories close by a human: %v", err)
	}
	if it := get(t, s, "S-001"); it.Status == model.Done {
		t.Fatal("story closed by an agent")
	}
}

func TestPolicyCommitsAndSamples(t *testing.T) {
	s := project(t)
	for _, kv := range [][2]string{{"GIT_AUTHOR_NAME", "t"}, {"GIT_AUTHOR_EMAIL", "t@t"}, {"GIT_COMMITTER_NAME", "t"}, {"GIT_COMMITTER_EMAIL", "t@t"}} {
		t.Setenv(kv[0], kv[1])
	}
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = s.Root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("add", ".")
	git("commit", "-qm", "init")
	writePolicy(t, s, "commit: auto\nsample: 1\n")
	readyTicket(t)
	mustShp(t, "agent:claude", "start", "T-0001")
	if err := os.WriteFile(filepath.Join(s.Root, "feature.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustShp(t, "agent:claude", "submit", "-m", "Add the feature", "--verified", "tests", "-f", "feature.go")
	reviewAs(t, "--pass")

	if it := get(t, s, "T-0001"); it.Status != model.Done {
		t.Fatalf("status %s", it.Status)
	}
	if msg := git("log", "-1", "--format=%B"); msg != "Add the feature\n\nTicket: T-0001" {
		t.Fatalf("commit message %q", msg)
	}
	files := git("show", "--name-only", "--format=", "HEAD")
	if !strings.Contains(files, "feature.go") || !strings.Contains(files, ".sherpa/tickets/T-0001.md") {
		t.Fatalf("committed files:\n%s", files)
	}

	if err := shp(t, "agent:claude", "policy", "audit", "T-0001", "-m", "fine"); err == nil {
		t.Fatal("agents must not audit")
	}
	mustShp(t, "human:ann", "policy", "audit", "T-0001", "-m", "fine")
	if err := shp(t, "human:ann", "policy", "audit", "T-0001"); err == nil {
		t.Fatal("audit notes are required")
	}
}

func TestPolicyInit(t *testing.T) {
	s := project(t)
	for _, f := range []string{"go.mod", "db/migrations/001.sql"} {
		p := filepath.Join(s.Root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustShp(t, "agent:claude", "policy", "init", "--commit", "auto", "--sample", "0.2", "--reviewer", "claude -p", "--auto-review")
	p, err := policy.Load(s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.Commit != "auto" || p.Sample != 0.2 || p.Verify != "go test ./..." || len(p.HumanReview) != 4 {
		t.Fatalf("drafted policy: %+v", p)
	}
	re, err := store.Find(s.Root)
	if err != nil {
		t.Fatal(err)
	}
	if re.Config.Reviewer.Command != "claude -p" || !re.Config.Reviewer.Auto || re.Config.Key != "CL" {
		t.Fatalf("config: %+v", re.Config)
	}
	if err := shp(t, "agent:claude", "policy", "init"); err == nil {
		t.Fatal("init must not overwrite without --force")
	}
	mustShp(t, "agent:claude", "policy", "init", "--force", "--verify", "")
	if p, _ := policy.Load(s.Dir); p.Verify != "" {
		t.Fatalf("--verify \"\" should clear the guess, got %q", p.Verify)
	}
}
