package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RL-Charles/sherpa/internal/model"
)

func load(t *testing.T, yaml string) (Policy, error) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, File), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	return Load(dir)
}

func ready(labels []string, parent string) *model.Item {
	return &model.Item{Kind: model.Ticket, Labels: labels, Parent: parent,
		Deliverables: []model.Deliverable{{Text: "outcome", Done: true}},
		Checks:       []model.Check{{Text: "unit", Done: true, Evidence: "abc"}}}
}

func TestNoPolicyClosesReadyTickets(t *testing.T) {
	p, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if p.Found || p.Commit != "" {
		t.Fatalf("missing file must be the zero policy: %+v", p)
	}
	if why := p.Human(ready(nil, ""), nil, Change{Lines: 5000}); why != "" {
		t.Fatalf("a ready ticket should close without a human by default: %s", why)
	}
}

func TestRejectsBadRules(t *testing.T) {
	for _, y := range []string{
		"human_review:\n  - name: all\n",
		"human_review:\n  - labels: [docs]\n",
		"commit: sometimes\n",
		"sample: 2\n",
		"auto_done:\n  - name: docs\n    labels: [docs]\n",
	} {
		if _, err := load(t, y); err == nil {
			t.Errorf("should refuse %q", y)
		}
	}
}

func TestHuman(t *testing.T) {
	p, err := load(t, `human_review:
  - name: migrations
    paths: ["db/migrations/**"]
  - name: deps
    paths: [go.mod]
  - name: flagged
    labels: [security]
  - name: big-tooling
    under: [e-1]
    min_lines: 100
`)
	if err != nil {
		t.Fatal(err)
	}
	all := []*model.Item{
		{ID: "E-001"}, {ID: "S-001", Parent: "E-001"}, {ID: "E-002"}, {ID: "S-002", Parent: "E-002"},
	}
	withCode := ready(nil, "S-002")
	withCode.Code = []model.CodeRef{{Path: "db/migrations/003_add.sql"}}
	for _, c := range []struct {
		name string
		it   *model.Item
		ch   Change
		why  string // "" closes
	}{
		{"plain", ready(nil, "S-002"), Change{Lines: 999, Files: []string{"main.go"}}, ""},
		{"migration file", ready(nil, "S-002"), Change{Files: []string{"db/migrations/001.sql"}}, "rule migrations: touches db/migrations/001.sql"},
		{"code map counts", withCode, Change{}, "rule migrations"},
		{"dep anywhere", ready(nil, "S-002"), Change{Files: []string{"tools/go.mod"}}, "rule deps"},
		{"label", ready([]string{"security"}, "S-002"), Change{}, "rule flagged: label security"},
		{"big under", ready(nil, "S-001"), Change{Lines: 100}, "rule big-tooling: under E-001, 100 lines"},
		{"small under", ready(nil, "S-001"), Change{Lines: 99}, ""},
		{"open check", &model.Item{Kind: model.Ticket,
			Deliverables: []model.Deliverable{{Text: "outcome", Done: true}},
			Checks:       []model.Check{{Text: "x"}}}, Change{}, "checks 0/1 unresolved"},
		{"manual", &model.Item{Kind: model.Ticket,
			Deliverables: []model.Deliverable{{Text: "outcome", Done: true}},
			Checks:       []model.Check{{Text: "x", Manual: true}}}, Change{}, "manual"},
		{"waived ok", &model.Item{Kind: model.Ticket,
			Deliverables: []model.Deliverable{{Text: "outcome", Done: true}},
			Checks:       []model.Check{{Text: "x", Waived: "n/a"}}}, Change{}, ""},
		{"story", &model.Item{Kind: model.Story,
			Deliverables: []model.Deliverable{{Text: "outcome", Done: true}},
			Checks:       []model.Check{{Text: "unit", Done: true}}}, Change{}, "stories and epics"},
		{"no deliverable", &model.Item{Kind: model.Ticket, Checks: []model.Check{{Text: "unit", Done: true}}}, Change{}, "deliverables"},
		{"no check", &model.Item{Kind: model.Ticket, Deliverables: []model.Deliverable{{Text: "outcome", Done: true}}}, Change{}, "checks"},
	} {
		why := p.Human(c.it, all, c.ch)
		if (c.why == "") != (why == "") || !strings.Contains(why, c.why) {
			t.Errorf("%s: got %q, want %q", c.name, why, c.why)
		}
	}
}

func TestMatch(t *testing.T) {
	for _, c := range []struct {
		glob, file string
		want       bool
	}{
		{"migrations/**", "migrations/001.sql", true},
		{"migrations/**", "migrations/a/b.sql", true},
		{"migrations/**", "app/migrations/001.sql", false},
		{"**/auth/**", "internal/auth/token.go", true},
		{"**/auth/**", "auth/token.go", true},
		{"**/auth/**", "internal/author.go", false},
		{"go.mod", "go.mod", true},
		{"go.mod", "tools/go.mod", true},
		{".env*", "config/.env.local", true},
		{"internal/*.go", "internal/x/y.go", false},
		{"./cmd/*", "cmd/main.go", true},
	} {
		if got := Match(c.glob, c.file); got != c.want {
			t.Errorf("Match(%q, %q) = %v", c.glob, c.file, got)
		}
	}
}

func TestSampled(t *testing.T) {
	if (Policy{}).Sampled("T-0001") {
		t.Fatal("no sample, no audit")
	}
	p := Policy{Sample: 0.25}
	n := 0
	for i := range 2000 {
		id := fmt.Sprintf("T-%04d", i)
		if p.Sampled(id) {
			n++
		}
		if p.Sampled(id) != p.Sampled(id) {
			t.Fatal("sampling must be stable per id")
		}
	}
	if n < 400 || n > 600 {
		t.Fatalf("sample 0.25 picked %d of 2000", n)
	}
	if !(Policy{Sample: 1}).Sampled("T-0001") {
		t.Fatal("sample 1 audits everything")
	}
}

func TestDraft(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"go.mod", "mise.toml", "db/migrations/001.sql", ".github/workflows/ci.yml",
		"internal/auth/token.go", "node_modules/x/migrations/no.sql", "web/package.json"} {
		path := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p := Draft(root)
	got := map[string]string{}
	for _, r := range p.HumanReview {
		got[r.Name] = fmt.Sprint(r.Paths, r.Labels, r.MinLines)
	}
	want := map[string]string{
		"migrations":   "[db/migrations/**] [] 0",
		"ship":         "[.github/workflows/**] [] 0",
		"dependencies": "[go.mod package.json] [] 0",
		"secrets":      "[**/auth/**] [] 0",
		"flagged":      "[] [security needs-human] 0",
		"large":        "[] [] 400",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("draft rules:\n got %v\nwant %v", got, want)
	}
	if p.Verify != "mise exec -- go test ./..." {
		t.Fatalf("verify = %q", p.Verify)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, File), []byte(Render(p)), 0o644); err != nil {
		t.Fatal(err)
	}
	back, err := Load(dir)
	if err != nil {
		t.Fatalf("rendered policy must load: %v", err)
	}
	if len(back.HumanReview) != 6 || back.Verify != p.Verify || back.Commit != CommitOff {
		t.Fatalf("round trip lost data: %+v", back)
	}
}
