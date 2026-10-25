package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/RL-Charles/sherpa/internal/model"
)

func TestDensityCycles(t *testing.T) {
	m := fixture(t)
	if m.density != densityCompact {
		t.Fatalf("default density = %s, want compact", m.density)
	}
	press(m, "v")
	if m.density != densityLabelled {
		t.Fatalf("after one v: %s, want labelled", m.density)
	}
	press(m, "v")
	if m.density != densityMinimal {
		t.Fatalf("after two v: %s, want minimal", m.density)
	}
	press(m, "v")
	if m.density != densityCompact {
		t.Fatalf("after three v: %s, want compact", m.density)
	}
}

func TestRowMetaPrioritisesChecksAndLabelsStatus(t *testing.T) {
	it := &model.Item{
		ID:       "T-0030",
		Status:   model.InProgress,
		Assignee: "claude",
		Updated:  time.Now(),
		Checks:   []model.Check{{Text: "one", Done: true}, {Text: "two"}},
		Links:    &model.Links{PRNumber: 35, PRState: "open"},
	}
	if got := ansi.Strip(rowMeta(it, densityCompact, 40)); !strings.HasPrefix(got, "1/2") || strings.Contains(got, "just now") || strings.Contains(got, "in progress") {
		t.Errorf("compact meta = %q, want checks (and PR) without age or status word", got)
	}
	got := ansi.Strip(rowMeta(it, densityLabelled, 40))
	for _, want := range []string{"1/2", "in progress", "@claude", "⇡"} {
		if !strings.Contains(got, want) {
			t.Errorf("labelled meta missing %q: %q", want, got)
		}
	}
}

func TestPinnedHeaderSurvivesScroll(t *testing.T) {
	m := fixture(t)
	press(m, "j", "j") // T-0001
	it, err := m.store.Get("T-0001")
	if err != nil {
		t.Fatal(err)
	}
	it.Body = strings.Repeat("A long paragraph so the body scrolls well past one screen.\n\n", 40)
	if err := m.store.Save(it); err != nil {
		t.Fatal(err)
	}
	m.reloadKeep()
	m.ed.scroll(300)
	if m.ed.offset == 0 {
		t.Fatal("body should be scrolled")
	}
	if m.ed.pinnedHeight() == 0 {
		t.Fatal("preview should reserve a pinned header")
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "T-0001 · Ticket one") {
		t.Fatalf("pinned title disappeared after scrolling:\n%s", view)
	}
}
