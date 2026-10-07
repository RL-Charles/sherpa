package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/RL-Charles/sherpa/internal/journal"
	"github.com/RL-Charles/sherpa/internal/model"
	"github.com/RL-Charles/sherpa/internal/store"
)

func fixture(t *testing.T) *Model {
	t.Helper()
	s, err := store.Init(t.TempDir(), "TST", "test")
	if err != nil {
		t.Fatal(err)
	}
	mk := func(k model.Kind, title, parent string) {
		if _, err := s.Create(store.NewItem{Kind: k, Title: title, Parent: parent, Description: "about " + title}); err != nil {
			t.Fatal(err)
		}
	}
	mk(model.Epic, "Epic one", "")
	mk(model.Story, "Story one", "E-001")
	mk(model.Ticket, "Ticket one", "S-001")
	mk(model.Ticket, "Ticket two", "S-001")
	mk(model.Epic, "Epic two", "")
	m := New(s, "dark")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	return m
}

func press(m *Model, keys ...string) {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		m.Update(msg)
	}
}

func TestNavigateAndPreview(t *testing.T) {
	m := fixture(t)
	if got := len(m.rows); got != 5 {
		t.Fatalf("want 5 visible rows (epics+story expanded), got %d", got)
	}
	press(m, "j", "j")
	if id := m.currentID(); id != "T-0001" {
		t.Fatalf("after jj want T-0001, got %s", id)
	}
	if !strings.Contains(ansi.Strip(m.View()), "about Ticket one") {
		t.Fatal("preview should show the selected ticket's description")
	}
	press(m, "h") // ticket → parent story
	if id := m.currentID(); id != "S-001" {
		t.Fatalf("h on a ticket should jump to parent, got %s", id)
	}
	press(m, "enter") // collapse story
	if got := len(m.rows); got != 3 {
		t.Fatalf("collapsed story should hide 2 tickets, got %d rows", got)
	}
	press(m, "C")
	if got, id := len(m.rows), m.currentID(); got != 2 || id != "E-001" {
		t.Fatalf("collapse all: want 2 rows on E-001, got %d on %s", got, id)
	}
	press(m, "E", "G")
	if id := m.currentID(); id != "E-002" {
		t.Fatalf("G should land on last row, got %s", id)
	}
}

func TestLiveReloadKeepsSelection(t *testing.T) {
	m := fixture(t)
	press(m, "j", "j", "j") // T-0002
	it, _ := m.store.Get("T-0002")
	it.Status = model.Review
	it.Body += "\ndid the thing\n"
	if err := m.store.Save(it); err != nil {
		t.Fatal(err)
	}
	m.Update(tickMsg{})
	if id := m.currentID(); id != "T-0002" {
		t.Fatalf("selection lost on reload: %s", id)
	}
	if !strings.Contains(ansi.Strip(m.View()), "did the thing") {
		t.Fatal("preview did not pick up the new worklog entry")
	}
}

func TestCoalescedRunes(t *testing.T) {
	m := fixture(t)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("jjj")})
	if id := m.currentID(); id != "T-0002" {
		t.Fatalf("coalesced jjj should move 3 rows, got %s", id)
	}
}

func TestPreviewShowsWorklog(t *testing.T) {
	m := fixture(t)
	for _, e := range []journal.Event{
		{Actor: "agent:claude", Session: "s1", Item: "T-0001", Type: "claim", From: "ready"},
		{Actor: "agent:claude", Session: "s1", Item: "T-0001", Type: "log", Kind: "decision", Message: "Pick the boring option"},
	} {
		if err := journal.Append(m.store.Dir, e); err != nil {
			t.Fatal(err)
		}
	}
	press(m, "r", "j", "j") // reload, select T-0001
	view := ansi.Strip(m.View())
	for _, want := range []string{"Worklog", "Session 1", "⚑ Pick the boring option", "in progress"} {
		if !strings.Contains(view, want) {
			t.Fatalf("preview missing %q:\n%s", want, view)
		}
	}
}
