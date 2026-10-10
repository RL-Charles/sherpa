package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/RL-Charles/sherpa/internal/model"
)

func rowKeys(m *Model) string {
	var keys []string
	for _, r := range m.rows {
		keys = append(keys, r.key())
	}
	return strings.Join(keys, " ")
}

func TestBoardGroupsByStatus(t *testing.T) {
	t.Setenv("SHERPA_ACTOR", "human:tester")
	m := fixture(t)
	press(m, "b")
	want := "status:in_progress status:agent_review status:review status:blocked status:ready status:backlog T-0001 T-0002 status:done"
	if got := rowKeys(m); got != want {
		t.Fatalf("board rows\n got %s\nwant %s", got, want)
	}
	if !strings.Contains(ansi.Strip(m.View()), "· board") {
		t.Fatal("header should say board")
	}
	press(m, "j", "j", "j", "j", "j") // backlog section
	if v := ansi.Strip(m.View()); !strings.Contains(v, "T-0001 Ticket one") || !strings.Contains(v, "S-001") {
		t.Fatalf("section preview should list its tickets:\n%s", v)
	}
	press(m, "j", "s", "3") // T-0001 → in progress; cursor follows it
	if m.currentID() != "T-0001" || m.rows[1].key() != "T-0001" {
		t.Fatalf("T-0001 should move under in progress with the cursor: %s (cursor on %s)", rowKeys(m), m.currentID())
	}
	press(m, "h") // to its section
	if m.currentID() != groupKey(model.InProgress) {
		t.Fatalf("h should jump to the section, got %s", m.currentID())
	}
	press(m, "enter") // collapse it
	if strings.Contains(rowKeys(m), "T-0001") {
		t.Fatalf("collapsed section should hide T-0001: %s", rowKeys(m))
	}
	press(m, "C", "b") // collapse the board, back to the tree
	if m.board || len(m.rows) != 5 {
		t.Fatalf("tree should keep its own expand state, rows=%d", len(m.rows))
	}
}

func TestBoardDoneStartsCollapsed(t *testing.T) {
	t.Setenv("SHERPA_ACTOR", "human:tester")
	m := fixture(t)
	press(m, "j", "j", "s", "7", "b")
	if strings.Contains(rowKeys(m), "T-0001") {
		t.Fatalf("done tickets should start hidden: %s", rowKeys(m))
	}
	if !strings.Contains(ansi.Strip(m.View()), "Done 1") {
		t.Fatal("done section should show its count")
	}
}
