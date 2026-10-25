package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/RL-Charles/sherpa/internal/gitinfo"
	"github.com/RL-Charles/sherpa/internal/journal"
	"github.com/RL-Charles/sherpa/internal/model"
	"github.com/RL-Charles/sherpa/internal/workflow"
)

// Needs-you inbox -------------------------------------------------------------
//
// `n` narrows the board to the review queues, human review first and, within
// it, items with ✋ manual checks first:
//
//	▾ ◑ Review 3
//	    ◑ T-0061 ✋ Manual checks…        a  approve → done (ticks its ✋ checks)
//	    ◑ S-004 Browse view…              x  send back with a note
//	▾ ◒ Agent review 2
//
// The header counts what waits for a human from every view.

var inboxOrder = []model.Status{model.Review, model.AgentReview}

type rejectPrompt struct {
	id   string
	note string
}

// needsYou counts items waiting for a human sign-off. Tickets wait for a
// review sign-off; stories and epics are human-only closures.
func (m *Model) needsYou() int {
	n := 0
	for _, it := range m.items {
		if it.Status == model.Review &&
			(it.Kind == model.Ticket || it.Kind == model.Story || it.Kind == model.Epic) {
			n++
		}
	}
	return n
}

func (m *Model) toggleInbox() {
	m.inbox = !m.inbox
	m.board = m.inbox
	if m.inbox {
		for _, st := range inboxOrder {
			m.expanded[groupKey(st)] = true
		}
	}
	m.focus = focusTree
	m.offset = 0
	m.rebuild(m.currentID())
	if m.inbox && m.selected() == nil && m.cursor+1 < len(m.rows) {
		m.move(1) // land on the first ticket, not the section header
	}
}

// reviewable returns the selected item if it waits for review.
func (m *Model) reviewable() *model.Item {
	it := m.selected()
	if it == nil {
		m.flash = "· pick an item"
		return nil
	}
	if it.Status != model.Review && it.Status != model.AgentReview {
		m.flash = "· " + it.ID + " is " + statusLabel(it.Status) + ", not in review"
		return nil
	}
	return it
}

// approve closes the selected item. Approving is the human's sign-off, so
// it ticks the ✋ checks still open, naming who verified them.
func (m *Model) approve() {
	it := m.reviewable()
	if it == nil {
		return
	}
	if err := workflow.CheckTransition(m.actor, it, model.Done); err != nil {
		m.flash = "⚠ " + err.Error()
		return
	}
	var ticked []string
	for _, n := range it.OpenManual() {
		c := &it.Checks[n-1]
		c.Done, c.Evidence = true, "verified by "+m.actor.Name
		ticked = append(ticked, fmt.Sprintf("✋ check %d: %s", n, c.Text))
	}
	from := it.Status
	it.Status = model.Done
	if err := m.store.Save(it); err != nil {
		m.flash = "⚠ " + err.Error()
		return
	}
	_ = journal.Append(m.store.Dir, journal.Event{
		Actor: m.actor.String(), Session: m.session, Item: it.ID, Type: "status",
		From: string(from), To: string(model.Done), Message: "approved from the needs-you inbox", Detail: ticked,
		Commit: gitinfo.Head(m.store.CodeRoot),
	})
	m.flash = "✓ " + it.ID + " approved · done"
	m.reloadKeep()
}

func (m *Model) openReject() {
	if it := m.reviewable(); it != nil {
		m.reject = &rejectPrompt{id: it.ID}
		m.layout()
	}
}

func (m *Model) rejectKey(msg tea.KeyMsg) {
	r := m.reject
	switch msg.Type {
	case tea.KeyEsc:
		m.reject = nil
		m.layout()
	case tea.KeyEnter:
		if strings.TrimSpace(r.note) == "" {
			m.flash = "⚠ say what needs fixing"
			return
		}
		m.reject = nil
		m.layout()
		m.sendBack(r.id, strings.TrimSpace(r.note))
	case tea.KeyBackspace:
		if rs := []rune(r.note); len(rs) > 0 {
			r.note = string(rs[:len(rs)-1])
		}
	case tea.KeyCtrlW:
		r.note = strings.TrimRight(r.note, " ")
		if i := strings.LastIndex(r.note, " "); i >= 0 {
			r.note = r.note[:i+1]
		} else {
			r.note = ""
		}
	case tea.KeySpace:
		r.note += " "
	case tea.KeyRunes:
		r.note += string(msg.Runes)
	}
}

// sendBack returns a ticket to in progress with the reviewer's note, as
// `shp review --reject` does.
func (m *Model) sendBack(id, note string) {
	it, err := m.store.Get(id)
	if err != nil {
		m.flash = "⚠ " + err.Error()
		return
	}
	if err := workflow.CheckTransition(m.actor, it, model.InProgress); err != nil {
		m.flash = "⚠ " + err.Error()
		return
	}
	from := it.Status
	it.Status = model.InProgress
	if err := m.store.Save(it); err != nil {
		m.flash = "⚠ " + err.Error()
		return
	}
	_ = journal.Append(m.store.Dir, journal.Event{
		Actor: m.actor.String(), Session: m.session, Item: id, Type: "review", Kind: "reject",
		From: string(from), To: string(model.InProgress), Message: note, Commit: gitinfo.Head(m.store.CodeRoot),
	})
	m.flash = "↩ " + id + " sent back"
	m.reloadKeep()
}

func (m *Model) rejectView() string {
	line := findPrompt.Render("Send "+m.reject.id+" back:") + " " + m.reject.note + "▏" +
		faint.Render("  enter send · esc cancel")
	return ansi.Truncate(line, max(1, m.width), "…")
}

// inboxKeys is the help shown in the inbox.
type inboxKeys struct{ k keyMap }

func (i inboxKeys) ShortHelp() []key.Binding {
	return []key.Binding{i.k.Up, i.k.Down, i.k.Approve, i.k.Reject, i.k.Focus,
		key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "back to tree")), i.k.Help, i.k.Quit}
}

func (i inboxKeys) FullHelp() [][]key.Binding { return [][]key.Binding{i.ShortHelp()} }
