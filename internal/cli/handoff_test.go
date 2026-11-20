package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/RL-Charles/sherpa/internal/journal"
	"github.com/RL-Charles/sherpa/internal/model"
	"github.com/RL-Charles/sherpa/internal/store"
)

func TestHandoffAndBriefReconcile(t *testing.T) {
	root := t.TempDir()
	s, err := store.Init(root, "HO", "handoff-test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(store.NewItem{Kind: model.Epic, Title: "E"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(store.NewItem{Kind: model.Story, Title: "S", Parent: "E-001"}); err != nil {
		t.Fatal(err)
	}
	it, err := s.Create(store.NewItem{Kind: model.Ticket, Title: "Auth refresh", Parent: "S-001", Description: "Keep users logged in."})
	if err != nil {
		t.Fatal(err)
	}
	it.Status = model.InProgress
	it.Code = []model.CodeRef{{Path: "internal/auth/token.go", Change: "modify", Status: "planned"}}
	if err := s.Save(it); err != nil {
		t.Fatal(err)
	}
	if err := s.SetActive(it.ID); err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(s.Dir, journal.Event{
		Actor: "agent:opencode", Session: "s1", Item: it.ID, Type: "touch",
		Files: []string{"internal/auth/token.go", "web/app.ts"},
	}); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()
	t.Setenv("SHERPA_ACTOR", "agent:opencode")
	t.Setenv("SHERPA_AGENT_SESSION", "ses_abc123")

	a := &app{}
	cmd := newRoot(a)
	cmd.SetArgs([]string{"handoff", "--now", "Rotation done; retry left", "--next", "Wire 401 retry in web/app.ts", "--blocker", "Waiting on test fixture"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("handoff failed: %v", err)
	}

	got, err := s.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Now != "Rotation done; retry left" {
		t.Fatalf("Now = %q", got.Now)
	}
	if got.Handoff == nil || got.Handoff.Next != "Wire 401 retry in web/app.ts" || got.Handoff.Blocker != "Waiting on test fixture" || got.Handoff.SessionID != "ses_abc123" {
		t.Fatalf("Handoff = %+v", got.Handoff)
	}

	evs, err := journal.ReadAll(s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	brief := BuildBriefWithEvents(got, journal.ForItem(evs, it.ID))
	if len(brief.Where) != 1 || brief.Where[0].Status != "touched" {
		t.Fatalf("Where should reconcile planned -> touched: %+v", brief.Where)
	}
	if len(brief.OffPlan) != 1 || brief.OffPlan[0] != "web/app.ts" {
		t.Fatalf("OffPlan = %+v, want [web/app.ts]", brief.OffPlan)
	}
	rendered := RenderBrief(brief)
	for _, want := range []string{"Next     Wire 401 retry", "Blocker  ⛔ Waiting on test fixture", "ses_abc123", "△ off-plan touched: web/app.ts"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("RenderBrief missing %q:\n%s", want, rendered)
		}
	}
}
