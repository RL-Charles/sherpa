package cli

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RL-Charles/sherpa/internal/journal"
	"github.com/RL-Charles/sherpa/internal/model"
)

func TestBuildBrief(t *testing.T) {
	it := &model.Item{
		ID: "T-0030", Title: "JWT refresh", Kind: model.Ticket, Status: model.Backlog,
		Template: "feature", Parent: "S-004", Assignee: "claude",
		Now: "rotation done, retry left",
		Body: "## Why\n\nSessions die mid-task when the access token expires.\n\n" +
			"## Context\n\n- Constraints: no schema change\n- Prior art: docs/auth.md\n\n" +
			"## Design\n\n### Shape\n\nAdapter around the token source.\n\n### Pattern\n\nAdapter + Clock.\n\n" +
			"## Out of scope\n\n- sign out everywhere\n",
		Deliverables: []model.Deliverable{{Text: "Refresh accepts an expired token"}},
		Checks: []model.Check{
			{Text: "unit: go test ./auth", For: 1},
			{Text: "e2e: two expiries", For: 1, Done: true, Evidence: "CI #42"},
		},
		Code: []model.CodeRef{{Path: "internal/auth/token.go", Change: "modify", Symbols: []string{"Rotate"}, Deliverable: 1, Status: "planned"}},
	}
	b := BuildBrief(it)
	if b.Outcome != "Sessions die mid-task when the access token expires." {
		t.Errorf("Outcome = %q", b.Outcome)
	}
	if b.NextCheck == "" || !strings.Contains(b.NextCheck, "unit: go test ./auth") {
		t.Errorf("NextCheck = %q", b.NextCheck)
	}
	if len(b.Context) == 0 || len(b.Design) == 0 || len(b.OutOfScope) == 0 {
		t.Errorf("missing context/design/out-of-scope: %+v", b)
	}
	text := RenderBrief(b)
	for _, want := range []string{"Outcome", "Now", "Deliverables", "Where", "Next check", "Context", "Design", "Out of scope"} {
		if !strings.Contains(text, want) {
			t.Errorf("brief missing %q:\n%s", want, text)
		}
	}
}

func TestMigrateOneBriefRecoversFactsOnly(t *testing.T) {
	it := &model.Item{
		ID: "T-0001", Title: "Refresh accepts an expired token", Kind: model.Ticket,
		Status: model.InProgress,
		Body:   "## Description\n\nSessions die when the token expires.\n",
		Checks: []model.Check{{Text: "unit: go test ./auth"}, {Text: "e2e: two expiries"}},
	}
	events := []journal.Event{
		{Item: "T-0001", Type: "log", Message: "Rotate on use", Files: []string{"internal/auth/token.go", "api/auth/handlers.go"}},
		{Item: "T-0001", Type: "touch", Files: []string{"internal/auth/token.go"}},
	}
	ch := migrateOneBrief(it, events, briefMigrateOptions{
		FromHistory: true, DeliverableFromTitle: true, LinkChecks: true,
		StampTemplate: true, NormalizeDescription: true,
	})
	if ch.Deliverable == "" || ch.LinkedChecks != 2 || len(ch.Files) != 2 {
		t.Fatalf("change = %+v", ch)
	}
	if it.Template != "feature" || it.Now != "Rotate on use" {
		t.Fatalf("template/now = %q/%q", it.Template, it.Now)
	}
	if why, ok := model.Section(it.Body, "Why"); !ok || !strings.Contains(why, "Sessions die") {
		t.Fatalf("Description was not moved to Why:\n%s", it.Body)
	}
	if _, ok := model.Section(it.Body, "Description"); ok {
		t.Fatalf("Description should be gone:\n%s", it.Body)
	}
	if len(it.Deliverables) != 1 || it.Checks[0].For != 1 || it.Checks[1].For != 1 {
		t.Fatalf("deliverable/check links = %+v / %+v", it.Deliverables, it.Checks)
	}
	if len(it.Code) != 2 || it.Code[0].Status != "touched" {
		t.Fatalf("code = %+v", it.Code)
	}
	// It must not invent Context/Design prose.
	if _, ok := model.Section(it.Body, "Context"); ok {
		t.Fatalf("migration invented Context:\n%s", it.Body)
	}
	if len(ch.RemainingGaps) == 0 {
		t.Fatal("remaining prose gaps should be reported")
	}
}

func TestMigrateBriefPreviewAndWrite(t *testing.T) {
	s := project(t)
	it := get(t, s, "T-0001")
	it.Status = model.InProgress
	it.Body = "## Description\n\nA real description.\n"
	it.Checks = []model.Check{{Text: "unit: go test ./..."}}
	if err := s.Save(it); err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(s.Dir, journal.Event{Item: "T-0001", Type: "touch", Files: []string{"internal/tui/tree.go"}}); err != nil {
		t.Fatal(err)
	}

	mustShp(t, "agent:claude", "migrate", "brief", "T-0001", "--deliverable-from-title", "--normalize-description")
	before := get(t, s, "T-0001")
	if len(before.Deliverables) != 0 || len(before.Code) != 0 {
		t.Fatalf("preview wrote changes: %+v %+v", before.Deliverables, before.Code)
	}

	mustShp(t, "agent:claude", "migrate", "brief", "T-0001", "--deliverable-from-title", "--normalize-description", "--write")
	after := get(t, s, "T-0001")
	if len(after.Deliverables) != 1 || after.Checks[0].For != 1 {
		t.Fatalf("deliverable not applied: %+v %+v", after.Deliverables, after.Checks)
	}
	if len(after.Code) != 1 || filepath.ToSlash(after.Code[0].Path) != "internal/tui/tree.go" {
		t.Fatalf("code not applied: %+v", after.Code)
	}
	if _, ok := model.Section(after.Body, "Why"); !ok {
		t.Fatalf("description not normalized:\n%s", after.Body)
	}
}

func TestMigrateBriefRefusesBulkWriteWithoutAll(t *testing.T) {
	project(t)
	err := shp(t, "agent:claude", "migrate", "brief", "--write")
	if err == nil || !strings.Contains(err.Error(), "--all") {
		t.Fatalf("bulk write should require --all, got %v", err)
	}
}

func TestBriefOutcomeFallsBack(t *testing.T) {
	it := &model.Item{ID: "T-1", Title: "Title only", Kind: model.Ticket, Status: model.Backlog, Created: time.Now()}
	if got := briefOutcome(it); got != "Title only" {
		t.Fatalf("outcome = %q", got)
	}
	it.Body = "## Why\n\nA real reason.\n"
	if got := briefOutcome(it); got != "A real reason." {
		t.Fatalf("outcome = %q", got)
	}
}
