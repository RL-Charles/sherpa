package policy

import (
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

func TestNoPolicyClosesNothing(t *testing.T) {
	p, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if rule, why := p.Closes(&model.Item{Labels: []string{"docs"}}, nil, 0); rule != "" || why == "" {
		t.Fatalf("missing policy must not auto-close: %q %q", rule, why)
	}
}

func TestRejectsCatchAllRules(t *testing.T) {
	if _, err := load(t, "auto_done:\n  - name: all\n    max_lines: 10\n"); err == nil {
		t.Fatal("a rule with no labels or under must be refused")
	}
	if _, err := load(t, "auto_done:\n  - labels: [docs]\n"); err == nil {
		t.Fatal("a rule needs a name")
	}
}

func TestCloses(t *testing.T) {
	p, err := load(t, `auto_done:
  - name: docs
    labels: [docs]
  - name: tooling
    under: [e-1]
    max_lines: 100
`)
	if err != nil {
		t.Fatal(err)
	}
	all := []*model.Item{
		{ID: "E-001"}, {ID: "S-001", Parent: "E-001"}, {ID: "E-002"}, {ID: "S-002", Parent: "E-002"},
	}
	ready := func(labels []string, parent string) *model.Item {
		return &model.Item{Kind: model.Ticket, Labels: labels, Parent: parent,
			Deliverables: []model.Deliverable{{Text: "outcome", Done: true}},
			Checks:       []model.Check{{Text: "unit", Done: true, Evidence: "abc"}}}
	}
	for _, c := range []struct {
		name  string
		it    *model.Item
		lines int
		want  string
		why   string
	}{
		{"label", ready([]string{"docs"}, "S-002"), 999, "docs", ""},
		{"ancestor", ready(nil, "S-001"), 50, "tooling", ""},
		{"too big", ready(nil, "S-001"), 101, "", "no auto_done rule"},
		{"elsewhere", ready(nil, "S-002"), 1, "", "no auto_done rule"},
		{"open check", &model.Item{Kind: model.Ticket, Labels: []string{"docs"}, Parent: "S-002",
			Deliverables: []model.Deliverable{{Text: "outcome", Done: true}},
			Checks:       []model.Check{{Text: "x"}}}, 0, "", "unresolved"},
		{"manual", &model.Item{Kind: model.Ticket, Labels: []string{"docs"}, Parent: "S-002",
			Deliverables: []model.Deliverable{{Text: "outcome", Done: true}},
			Checks:       []model.Check{{Text: "x", Manual: true}}}, 0, "", "manual"},
		{"waived ok", &model.Item{Kind: model.Ticket, Labels: []string{"docs"}, Parent: "S-002",
			Deliverables: []model.Deliverable{{Text: "outcome", Done: true}},
			Checks:       []model.Check{{Text: "x", Waived: "n/a"}}}, 0, "docs", ""},
		{"story", &model.Item{Kind: model.Story, Labels: []string{"docs"}, Parent: "S-002",
			Deliverables: []model.Deliverable{{Text: "outcome", Done: true}},
			Checks:       []model.Check{{Text: "unit", Done: true}}}, 0, "", "only tickets"},
		{"no deliverable", &model.Item{Kind: model.Ticket, Labels: []string{"docs"}, Parent: "S-002",
			Checks: []model.Check{{Text: "unit", Done: true}}}, 0, "", "deliverables"},
		{"no check", &model.Item{Kind: model.Ticket, Labels: []string{"docs"}, Parent: "S-002",
			Deliverables: []model.Deliverable{{Text: "outcome", Done: true}}}, 0, "", "checks"},
	} {
		rule, why := p.Closes(c.it, all, c.lines)
		if rule != c.want || !strings.Contains(why, c.why) {
			t.Errorf("%s: got %q (%s), want %q (%s)", c.name, rule, why, c.want, c.why)
		}
	}
}
