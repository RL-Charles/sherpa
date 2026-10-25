package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/RL-Charles/sherpa/internal/live"
)

func TestWorkingSpinner(t *testing.T) {
	m := fixture(t)
	if cmd := m.refreshPulses(); cmd != nil || m.spinning {
		t.Fatal("no agent working: the animation must not run")
	}
	_ = live.Touch(m.store, "claude", "T-0001")
	m.Update(tickMsg{})
	if !m.spinning {
		t.Fatal("a fresh heartbeat should start the animation")
	}
	v := ansi.Strip(m.View())
	if !strings.Contains(v, spinFrames[0]+" T-0001 Ticket one") || !strings.Contains(v, "claude on T-0001") {
		t.Fatalf("T-0001 should spin and the header name the agent:\n%s", v)
	}
	if strings.Contains(v, spinFrames[0]+" T-0002") {
		t.Fatal("only the worked ticket spins")
	}
	m.Update(spinMsg{})
	if !strings.Contains(ansi.Strip(m.View()), spinFrames[1]+" T-0001") {
		t.Fatal("spin should advance the frame")
	}
	_ = live.Clear(m.store)
	m.Update(tickMsg{})
	if _, cmd := m.Update(spinMsg{}); cmd != nil || m.spinning {
		t.Fatal("animation should stop once the agent is done")
	}
	if strings.Contains(ansi.Strip(m.View()), "claude on") {
		t.Fatal("header should drop the agent")
	}
}

func TestWorkingSpinnerMarksAncestors(t *testing.T) {
	m := fixture(t)
	_ = live.Touch(m.store, "claude", "T-0001")
	m.Update(tickMsg{})
	v := ansi.Strip(m.View())
	for _, id := range []string{"E-001", "S-001", "T-0001"} {
		if !strings.Contains(v, spinFrames[0]+" "+id) {
			t.Fatalf("%s should spin while its ticket is worked on:\n%s", id, v)
		}
	}
	for _, id := range []string{"E-002", "T-0002"} {
		if strings.Contains(v, spinFrames[0]+" "+id) {
			t.Fatalf("%s is not on the worked path and must not spin", id)
		}
	}
	m.expanded = map[string]bool{}
	m.rebuild("E-001")
	if v := ansi.Strip(m.View()); !strings.Contains(v, spinFrames[0]+" E-001") {
		t.Fatalf("a collapsed epic should still spin:\n%s", v)
	}
}
