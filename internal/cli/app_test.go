package cli

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/RL-Charles/sherpa/internal/actions"
	"github.com/RL-Charles/sherpa/internal/journal"
	"github.com/RL-Charles/sherpa/internal/live"
	"github.com/RL-Charles/sherpa/internal/model"
	"github.com/RL-Charles/sherpa/internal/store"
	"github.com/RL-Charles/sherpa/internal/workflow"
)

func TestAppFeed(t *testing.T) {
	s, err := store.Init(t.TempDir(), "AP", "app-feed")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []store.NewItem{
		{Kind: model.Epic, Title: "Epic"},
		{Kind: model.Story, Title: "Story", Parent: "E-001"},
		{Kind: model.Ticket, Title: "Work", Parent: "S-001", Labels: []string{"security"}},
		{Kind: model.Ticket, Title: "Shipped", Parent: "S-001"},
	} {
		if _, err := s.Create(n); err != nil {
			t.Fatal(err)
		}
	}
	it, _ := s.Get("T-0001")
	it.Status = model.InProgress
	it.Code = []model.CodeRef{{Path: "a.go"}}
	it.Handoff = &model.Handoff{Next: "write the test"}
	_ = s.Save(it)
	done, _ := s.Get("T-0002")
	done.Status = model.Done
	_ = s.Save(done)
	_ = journal.Append(s.Dir, journal.Event{Actor: "agent:opencode", Session: "x", Item: "T-0001", Type: "claim"})
	_ = journal.Append(s.Dir, journal.Event{Actor: "agent:opencode", Session: "x", Item: "T-0001", Type: "touch", Files: []string{"a.go", "b.go"}})
	_ = journal.Append(s.Dir, journal.Event{Actor: "agent:opencode", Session: "x", Item: "T-0001", Type: "log", Kind: journal.KindDecision, Message: "Keep it simple"})
	_ = live.TouchInfo(s, live.Pulse{Agent: "opencode", Ticket: "T-0001", Action: "edit", File: "a.go"})

	snap, err := buildAppSnapshot(s, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Items) != 4 || snap.Counts["in_progress"] != 1 || snap.Counts["done"] != 1 {
		t.Fatalf("snapshot items/counts wrong: %d %v", len(snap.Items), snap.Counts)
	}
	if len(snap.Pulses) != 1 || snap.Pulses[0].Ticket != "T-0001" || snap.Pulses[0].File != "a.go" {
		t.Fatalf("pulses = %+v", snap.Pulses)
	}
	var story, work appRow
	for _, r := range snap.Items {
		switch r.ID {
		case "S-001":
			story = r
		case "T-0001":
			work = r
		}
	}
	if story.RollupDone != 1 || story.RollupTotal != 2 {
		t.Fatalf("story rollup = %d/%d", story.RollupDone, story.RollupTotal)
	}
	if !work.Security || work.Next != "write the test" {
		t.Fatalf("ticket row = %+v", work)
	}

	item, err := buildAppItem(s, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(item.Crumbs) != 2 || item.Crumbs[0].ID != "E-001" || item.Crumbs[1].ID != "S-001" {
		t.Fatalf("crumbs = %+v", item.Crumbs)
	}
	if len(item.Where) != 1 || item.Where[0].Status != "touched" || len(item.OffPlan) != 1 || item.OffPlan[0] != "b.go" {
		t.Fatalf("where = %+v off = %v", item.Where, item.OffPlan)
	}
	if len(item.Signals) != 1 || item.Signals[0].Headline != "Keep it simple" {
		t.Fatalf("signals = %+v", item.Signals)
	}
	if len(item.Sessions) != 1 || item.Sessions[0].Title != "Session 1" || !item.Sessions[0].Open {
		t.Fatalf("sessions = %+v", item.Sessions)
	}
}

func TestAppActionsShareTheTUIRules(t *testing.T) {
	s, err := store.Init(t.TempDir(), "AA", "app-actions")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []store.NewItem{
		{Kind: model.Epic, Title: "E"}, {Kind: model.Story, Title: "S", Parent: "E-001"},
		{Kind: model.Ticket, Title: "T", Parent: "S-001"},
	} {
		if _, err := s.Create(n); err != nil {
			t.Fatal(err)
		}
	}
	human := actions.Who{Actor: workflow.Actor{Kind: workflow.Human, Name: "pat"}, Session: "s"}
	if _, err := actions.SetStatus(s, human, "T-0001", model.Review); err != nil {
		t.Fatal(err)
	}
	it, _ := s.Get("T-0001")
	it.Checks = []model.Check{{Text: "looks right", Manual: true}}
	_ = s.Save(it)
	if _, err := actions.SendBack(s, human, "T-0001", ""); err == nil {
		t.Fatal("a send-back needs a note")
	}
	if _, err := actions.Approve(s, human, "T-0001", ""); err != nil {
		t.Fatal(err)
	}
	it, _ = s.Get("T-0001")
	if it.Status != model.Done || !it.Checks[0].Done || it.Checks[0].Evidence != "verified by pat" {
		t.Fatalf("approve should close and tick manual checks: %+v", it)
	}
	agent := actions.Who{Actor: workflow.Actor{Kind: workflow.Agent, Name: "bot"}, Session: "s"}
	if _, err := actions.SetStatus(s, agent, "T-0001", model.Ready); err == nil {
		t.Fatal("agents cannot reopen done work")
	}
	base := it.Body
	_, _, _ = actions.SaveBody(s, human, "T-0001", base, base+"agent note\n", "test", true)
	body, merged, err := actions.SaveBody(s, human, "T-0001", base, "## Mine\n", "test", false)
	if err != nil || !merged || !strings.Contains(body, "agent note") {
		t.Fatalf("appended lines should merge: %v %v %q", err, merged, body)
	}
	if _, _, err := actions.SaveBody(s, human, "T-0001", "something else", "x", "test", false); !errors.Is(err, actions.ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
	evs, _ := journal.ReadAll(s.Dir)
	if len(evs) < 4 {
		t.Fatalf("every change is journaled, got %d events", len(evs))
	}
}
