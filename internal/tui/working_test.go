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
