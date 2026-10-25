package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/RL-Charles/sherpa/internal/live"
)

// Live "agent working" indicator ----------------------------------------------
//
// Agent hooks keep a heartbeat per checkout (package live). While any is
// fresh, a spinner replaces the status glyph of the ticket being worked on
// and of its story and epic, and the header names the agent. The animation timer only runs while
// someone is working, so an idle TUI does not redraw.

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const spinEvery = 100 * time.Millisecond

var workStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "162", Dark: "205"}).Bold(true)

type spinMsg struct{}

func spin() tea.Cmd { return tea.Tick(spinEvery, func(time.Time) tea.Msg { return spinMsg{} }) }

// refreshPulses re-reads the heartbeats and starts the animation if needed.
func (m *Model) refreshPulses() tea.Cmd {
	m.pulses = live.Read(m.store.Dir, time.Now())
	if len(m.pulses) > 0 && !m.spinning {
		m.spinning = true
		return spin()
	}
	return nil
}

// onSpin advances the animation, stopping the timer once nobody is working.
func (m *Model) onSpin() tea.Cmd {
	if len(m.pulses) == 0 {
		m.spinning = false
		return nil
	}
	m.frame = (m.frame + 1) % len(spinFrames)
	return spin()
}

// spinner is the current frame if an agent is working on id or anything
// under it, else "". Parents spin too, so a collapsed epic or story still
// shows that work is happening inside it.
func (m *Model) spinner(id string) string {
	for _, p := range m.pulses {
		seen := map[string]bool{}
		for cur := p.Ticket; cur != "" && !seen[cur]; {
			if cur == id {
				return spinFrames[m.frame]
			}
			seen[cur] = true
			it := m.findItem(cur)
			if it == nil {
				break
			}
			cur = it.Parent
		}
	}
	return ""
}

// workingView is the header's "⠹ claude on T-0009" segment.
func (m *Model) workingView() string {
	if len(m.pulses) == 0 {
		return ""
	}
	parts := make([]string, 0, len(m.pulses))
	for _, p := range m.pulses {
		s := p.Agent
		if p.Ticket != "" {
			s += " on " + p.Ticket
		}
		parts = append(parts, s)
	}
	return workStyle.Render(spinFrames[m.frame]) + " " + strings.Join(parts, ", ")
}
