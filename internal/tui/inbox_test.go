package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/RL-Charles/sherpa/internal/journal"
	"github.com/RL-Charles/sherpa/internal/model"
)

// inboxFixture puts T-0001 (with an open ✋ check) and T-0002 in review.
func inboxFixture(t *testing.T) *Model {
	t.Helper()
	t.Setenv("SHERPA_ACTOR", "human:ann")
	m := fixture(t)
	for _, id := range []string{"T-0002", "T-0001"} {
		it, _ := m.store.Get(id)
		it.Status = model.Review
		if id == "T-0001" {
			it.Checks = []model.Check{{Text: "auto", Done: true}, {Text: "looks right", Manual: true}}
		}
		if err := m.store.Save(it); err != nil {
			t.Fatal(err)
		}
	}
	m.reloadKeep()
	return m
}

func TestInboxListsReviewHandChecksFirst(t *testing.T) {
	m := inboxFixture(t)
	if v := ansi.Strip(m.View()); !strings.Contains(v, "✋ 2 need you") {
		t.Fatalf("header should count review work:\n%s", v)
	}
	press(m, "n")
	if !m.inbox || m.currentID() != "T-0001" {
		t.Fatalf("inbox should open on the ✋ ticket, got %q", m.currentID())
	}
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "needs you") || !strings.Contains(v, "T-0001 ✋ Ticket one") {
		t.Fatalf("inbox view:\n%s", v)
	}
	if strings.Index(v, "T-0001") > strings.Index(v, "T-0002") {
		t.Fatal("tickets with manual checks come first")
	}
	if strings.Contains(v, "Epic one") {
		t.Fatal("the inbox shows only review queues")
	}
	press(m, "n")
	if m.inbox || m.board {
		t.Fatal("n again goes back to the tree")
	}
}

func TestInboxIncludesStoriesAwaitingHumanClose(t *testing.T) {
	t.Setenv("SHERPA_ACTOR", "human:ann")
	m := fixture(t)
	s, _ := m.store.Get("S-001")
	s.Status = model.Review
	if err := m.store.Save(s); err != nil {
		t.Fatal(err)
	}
	m.reloadKeep()
	if v := ansi.Strip(m.View()); !strings.Contains(v, "✋ 1 needs you") {
		t.Fatalf("header should count the story:\n%s", v)
	}
	press(m, "n")
	if m.currentID() != "S-001" {
		t.Fatalf("inbox should land on the story, got %q", m.currentID())
	}
	press(m, "a")
	if got, _ := m.store.Get("S-001"); got.Status != model.Done {
		t.Fatalf("a human should close the story from the inbox, got %s", got.Status)
	}
}

func TestInboxApprove(t *testing.T) {
	m := inboxFixture(t)
	press(m, "n", "a")
	it, _ := m.store.Get("T-0001")
	if it.Status != model.Done {
		t.Fatalf("a should close it, got %s", it.Status)
	}
	if c := it.Checks[1]; !c.Done || c.Evidence != "verified by ann" {
		t.Fatalf("approving signs off the ✋ check: %+v", c)
	}
	events, _ := journal.ReadAll(m.store.Dir)
	if e := events[len(events)-1]; e.Type != "status" || e.To != "done" || e.Actor != "human:ann" {
		t.Fatalf("approval should be journaled: %+v", e)
	}
}

func TestInboxApproveRefusedForAgents(t *testing.T) {
	m := inboxFixture(t)
	t.Setenv("SHERPA_ACTOR", "agent:claude")
	m2 := New(m.store, "dark")
	m2.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	press(m2, "n", "a")
	if it, _ := m.store.Get("T-0001"); it.Status != model.Review {
		t.Fatalf("an agent's TUI must not approve, got %s", it.Status)
	}
}

func TestInboxReject(t *testing.T) {
	m := inboxFixture(t)
	press(m, "n", "j", "x")
	if m.reject == nil || m.reject.id != "T-0002" {
		t.Fatal("x should ask why T-0002 is sent back")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.reject == nil {
		t.Fatal("an empty note must not send it back")
	}
	press(m, "needs")
	m.Update(tea.KeyMsg{Type: tea.KeySpace})
	press(m, "tests")
	if !strings.Contains(ansi.Strip(m.View()), "Send T-0002 back: needs tests") {
		t.Fatalf("prompt should echo the note:\n%s", ansi.Strip(m.View()))
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	it, _ := m.store.Get("T-0002")
	if it.Status != model.InProgress {
		t.Fatalf("reject should send it back to in progress, got %s", it.Status)
	}
	events, _ := journal.ReadAll(m.store.Dir)
	if e := events[len(events)-1]; e.Type != "review" || e.Kind != "reject" || e.Message != "needs tests" {
		t.Fatalf("rejection should be journaled with the note: %+v", e)
	}
}
