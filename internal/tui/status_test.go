package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/RL-Charles/sherpa/internal/journal"
	"github.com/RL-Charles/sherpa/internal/model"
)

func status(t *testing.T, m *Model, id string) model.Status {
	t.Helper()
	it, err := m.store.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return it.Status
}

func TestStatusPickerKeys(t *testing.T) {
	t.Setenv("SHERPA_ACTOR", "human:tester")
	m := fixture(t)
	press(m, "j", "j", "s") // T-0001
	if m.pick == nil || !strings.Contains(ansi.Strip(m.View()), "Move T-0001 to") {
		t.Fatal("s should open the picker in the footer")
	}
	press(m, "l", "l", "enter") // backlog → ready → in progress
	if got := status(t, m, "T-0001"); got != model.InProgress {
		t.Fatalf("want in_progress, got %s", got)
	}
	if m.pick != nil {
		t.Fatal("picker should close after applying")
	}
	press(m, "s", "5") // review
	if got := status(t, m, "T-0001"); got != model.Review {
		t.Fatalf("want review, got %s", got)
	}
	press(m, "s", "7") // done, as a human
	if got := status(t, m, "T-0001"); got != model.Done {
		t.Fatalf("want done, got %s", got)
	}
	events, _ := journal.ReadAll(m.store.Dir)
	var moves []string
	for _, e := range journal.ForItem(events, "T-0001") {
		if e.Type == "status" {
			moves = append(moves, e.From+">"+e.To)
		}
	}
	if want := "backlog>in_progress in_progress>review review>done"; strings.Join(moves, " ") != want {
		t.Fatalf("journal moves %v, want %s", moves, want)
	}
	press(m, "s")
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.pick != nil || status(t, m, "T-0001") != model.Done {
		t.Fatal("esc should cancel without changes")
	}
}

func TestStatusPickerAgentCannotFinish(t *testing.T) {
	t.Setenv("SHERPA_ACTOR", "agent:tester")
	m := fixture(t)
	press(m, "j", "j", "s", "7")
	if got := status(t, m, "T-0001"); got == model.Done || !strings.HasPrefix(m.flash, "⚠") {
		t.Fatalf("agent TUI must not mark done: status=%s flash=%q", got, m.flash)
	}
}

func TestStatusPickerClick(t *testing.T) {
	t.Setenv("SHERPA_ACTOR", "human:tester")
	m := fixture(t)
	press(m, "j", "j", "s")
	x, y := find(t, m, "5 ◑ review", 0)
	click(m, x+1, y)
	if got := status(t, m, "T-0001"); got != model.Review {
		t.Fatalf("click on review: got %s", got)
	}
}
