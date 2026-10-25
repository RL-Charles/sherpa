// Package tui is Sherpa's full-screen terminal UI: an item tree on the left
// and a live-preview markdown pane on the right that renders like Obsidian —
// the block under the cursor is raw, editable text; the rest stays rendered.
package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"

	"github.com/RL-Charles/sherpa/internal/journal"
	"github.com/RL-Charles/sherpa/internal/live"
	"github.com/RL-Charles/sherpa/internal/model"
	"github.com/RL-Charles/sherpa/internal/render"
	"github.com/RL-Charles/sherpa/internal/store"
	"github.com/RL-Charles/sherpa/internal/workflow"
	"github.com/RL-Charles/sherpa/internal/worklog"
)

// reloadEvery is how often the TUI re-reads .sherpa/ so agent activity shows up live.
const reloadEvery = 2 * time.Second

type focus int

const (
	focusTree focus = iota
	focusPreview
)

type tickMsg struct{}

type Model struct {
	store   *store.Store
	actor   workflow.Actor
	session string

	items     []*model.Item
	events    []journal.Event
	expandLog bool // show every worklog session, not just the latest
	rows      []row
	expanded  map[string]bool // tree items by ID, board sections by groupKey
	board     bool            // left pane shows the status board instead of the tree
	inbox     bool            // …narrowed to the review queues (board is set too)
	reject    *rejectPrompt   // asking why a ticket is sent back, open while non-nil
	cursor    int
	offset    int
	freeTree  bool // wheel-scrolled: offset no longer follows the cursor
	density   density

	ed        *editor
	previewID string

	// Edit session state. editBase is the body as it was on disk when editing
	// began; if the disk copy no longer matches at save time, someone else
	// (usually an agent) changed it and we refuse to overwrite silently.
	editID    string
	editBase  string
	conflict  bool
	quitArmed bool
	flash     string
	pick      *picker // status picker, open while non-nil
	find      *finder // fuzzy finder, open while non-nil

	pulses   []live.Pulse // agents working right now
	spinning bool         // the spinner timer is running
	frame    int

	focus    focus
	width    int
	height   int
	keys     keyMap
	help     help.Model
	showHelp bool
	err      error
}

// New builds the model. style is a Glamour standard style ("dark" or "light").
func New(s *store.Store, style string) *Model {
	actor := workflow.CurrentActor()
	m := &Model{
		store:    s,
		actor:    actor,
		session:  journal.Session(actor.String()),
		expanded: map[string]bool{},
		density:  densityCompact,
		ed:       newEditor(style),
		keys:     defaultKeys(),
		help:     help.New(),
	}
	m.reload()
	for _, it := range m.items {
		if it.Kind != model.Ticket {
			m.expanded[it.ID] = true
		}
	}
	for _, st := range boardOrder {
		m.expanded[groupKey(st)] = st != model.Done
	}
	m.rebuild("")
	return m
}

// Run starts the TUI in the alternate screen.
func Run(s *store.Store) error {
	style := styles.LightStyle
	if lipgloss.HasDarkBackground() {
		style = styles.DarkStyle
	}
	_, err := tea.NewProgram(New(s, style), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}

func (m *Model) Init() tea.Cmd { return tea.Batch(tick(), m.refreshPulses()) }

func tick() tea.Cmd {
	return tea.Tick(reloadEvery, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *Model) reload() {
	items, err := m.store.All()
	if err == nil {
		m.items = items
		var events []journal.Event
		if events, err = journal.ReadAll(m.store.Dir); err == nil {
			m.events = events
		}
	}
	m.err = err
}

func (m *Model) selected() *model.Item {
	if m.find != nil {
		return m.found()
	}
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		return m.rows[m.cursor].item
	}
	return nil
}

// rebuild recomputes visible rows, keeping the cursor on keepID if it is still visible.
func (m *Model) rebuild(keepID string) {
	if m.board {
		order := boardOrder
		if m.inbox {
			order = inboxOrder
			m.rows = flattenInbox(m.items, m.expanded, order)
		} else {
			m.rows = flattenBoard(m.items, m.expanded, order)
		}
	} else {
		m.rows = flatten(m.items, m.expanded)
	}
	if keepID != "" {
		for i, r := range m.rows {
			if r.key() == keepID {
				m.cursor = i
				break
			}
		}
	}
	m.cursor = max(0, min(m.cursor, len(m.rows)-1))
	m.clampOffset()
	m.refreshPreview()
}

func (m *Model) reloadKeep() {
	m.reload()
	m.rebuild(m.currentID())
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.Width = msg.Width
		m.layout()
		m.refreshPreview()
		return m, nil

	case tickMsg:
		if !m.ed.editing { // never swap the document out from under the cursor
			m.reloadKeep()
		}
		return m, tea.Batch(tick(), m.refreshPulses())

	case spinMsg:
		return m, m.onSpin()

	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			m.click(msg.X, msg.Y)
			return m, nil
		}
		if msg.X >= m.treeWidth() {
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				m.ed.scroll(-3)
			case tea.MouseButtonWheelDown:
				m.ed.scroll(3)
			}
			return m, nil
		}
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.scrollTree(-1)
		case tea.MouseButtonWheelDown:
			m.scrollTree(1)
		}
		return m, nil

	case tea.KeyMsg:
		m.freeTree = false // keys bring the selection back into view
		if !m.conflict {
			m.flash = ""
		}
		if m.ed.editing {
			return m, m.editKey(msg)
		}
		// Bubble Tea v1 coalesces runes read together ("jjj" from key repeat
		// or scripted input) into one message; replay them one at a time.
		if msg.Type == tea.KeyRunes && len(msg.Runes) > 1 && !msg.Paste {
			var cmds []tea.Cmd
			for _, r := range msg.Runes {
				_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}, Alt: msg.Alt})
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		}
		if m.pick != nil {
			m.pickKey(msg)
			return m, nil
		}
		if m.find != nil {
			m.findKey(msg)
			return m, nil
		}
		if m.reject != nil {
			m.rejectKey(msg)
			return m, nil
		}
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Help):
			m.showHelp = !m.showHelp
			m.help.ShowAll = m.showHelp
			m.layout()
			return m, nil
		case key.Matches(msg, m.keys.Focus):
			if m.focus == focusTree {
				m.focus = focusPreview
			} else {
				m.focus = focusTree
			}
			m.layout()
			return m, nil
		case key.Matches(msg, m.keys.Reload):
			m.reloadKeep()
			return m, nil
		case key.Matches(msg, m.keys.Edit):
			m.startEdit()
			return m, nil
		case key.Matches(msg, m.keys.Worklog):
			m.expandLog = !m.expandLog
			m.refreshPreview()
			return m, nil
		case key.Matches(msg, m.keys.Status):
			m.openPicker()
			return m, nil
		case key.Matches(msg, m.keys.Board):
			m.toggleBoard()
			return m, nil
		case key.Matches(msg, m.keys.Density):
			m.density = m.density.next()
			m.flash = "· tree: " + m.density.String()
			m.rebuild(m.currentID())
			return m, nil
		case key.Matches(msg, m.keys.Find):
			m.openFinder()
			return m, nil
		case key.Matches(msg, m.keys.Inbox):
			m.toggleInbox()
			return m, nil
		case m.inbox && key.Matches(msg, m.keys.Approve):
			m.approve()
			return m, nil
		case m.inbox && key.Matches(msg, m.keys.Reject):
			m.openReject()
			return m, nil
		}
		if m.focus == focusPreview {
			m.previewKey(msg)
			return m, nil
		}
		m.treeKey(msg)
	}
	return m, nil
}

func (m *Model) previewKey(msg tea.KeyMsg) {
	switch {
	case msg.Type == tea.KeyEsc:
		m.focus = focusTree
		m.layout()
	case msg.Type == tea.KeyEnter || msg.String() == "i":
		m.startEdit()
	case key.Matches(msg, m.keys.Up):
		m.ed.scroll(-1)
	case key.Matches(msg, m.keys.Down):
		m.ed.scroll(1)
	case key.Matches(msg, m.keys.PageUp):
		m.ed.scroll(-m.ed.height / 2)
	case key.Matches(msg, m.keys.PageDown):
		m.ed.scroll(m.ed.height / 2)
	case key.Matches(msg, m.keys.Top):
		m.ed.scrollTo(true)
	case key.Matches(msg, m.keys.Bottom):
		m.ed.scrollTo(false)
	}
}

// Edit mode -----------------------------------------------------------------

func (m *Model) startEdit() {
	it := m.selected()
	if it == nil {
		return
	}
	m.focus = focusPreview
	m.editID, m.editBase = it.ID, it.Body
	m.conflict, m.quitArmed = false, false
	m.ed.lead = "" // editing gets the pane; the review card returns after save/discard
	m.layout()
	m.ed.startEditing()
}

func (m *Model) stopEdit() {
	m.ed.editing = false
	m.conflict, m.quitArmed = false, false
	m.layout()
	m.reloadKeep()
}

func (m *Model) editKey(msg tea.KeyMsg) tea.Cmd {
	if msg.Type == tea.KeyCtrlS { // works in every vim mode
		m.save(m.conflict) // a second ctrl+s after a conflict overwrites
		return nil
	}
	act := m.ed.vimKey(msg)
	if w := m.ed.vim.msg; w != "" {
		m.flash = "⚠ " + w
	}
	switch act {
	case actSave:
		m.save(false)
	case actForceSave:
		m.save(true)
	case actSaveExit, actForceSaveExit:
		if m.save(act == actForceSaveExit) {
			m.stopEdit()
		}
	case actExit:
		m.stopEdit()
	case actDiscard:
		m.flash = "↺ discarded your changes to " + m.editID
		m.ed.dirty = false
		m.stopEdit()
	}
	m.layout() // help bar height differs per vim mode
	return nil
}

// save writes the edited body back. Only the body is replaced, so status or
// assignee changes an agent made meanwhile survive. If the body itself
// changed on disk since editing began, it refuses unless force is set.
func (m *Model) save(force bool) bool {
	if !m.ed.dirty {
		return true
	}
	cur, err := m.store.Get(m.editID)
	if err != nil {
		m.flash = "⚠ " + err.Error()
		return false
	}
	body, merged := m.ed.body(), false
	if cur.Body != m.editBase && !force {
		// The common case is an agent appending worklog lines: keep them,
		// after the user's version. Anything else is a real conflict.
		base := strings.TrimRight(m.editBase, "\n")
		if !strings.HasPrefix(cur.Body, base) {
			m.conflict = true
			m.flash = "⚠ " + m.editID + " changed on disk while you edited · :w! overwrite · :q! discard yours"
			return false
		}
		body = strings.TrimRight(body, "\n") + cur.Body[len(base):]
		merged = true
	}
	cur.Body = body
	if merged {
		m.ed.lines = strings.Split(strings.TrimRight(body, "\n"), "\n")
	}
	if err := m.store.Save(cur); err != nil {
		m.flash = "⚠ " + err.Error()
		return false
	}
	_ = journal.Append(m.store.Dir, journal.Event{
		Actor: m.actor.String(), Session: m.session, Item: cur.ID, Type: "edit", Message: "edited body in TUI",
	})
	m.editBase = cur.Body
	m.ed.dirty, m.conflict = false, false
	m.flash = "✓ saved " + cur.ID
	if merged {
		m.flash += " · kept new lines added on disk meanwhile"
	}
	if p := model.GoalProblem(cur); p != "" {
		m.flash += " · ⚠ " + p
	}
	return true
}

// Tree ----------------------------------------------------------------------

func (m *Model) treeKey(msg tea.KeyMsg) {
	switch {
	case key.Matches(msg, m.keys.Up):
		m.move(-1)
	case key.Matches(msg, m.keys.Down):
		m.move(1)
	case key.Matches(msg, m.keys.PageUp):
		m.move(-m.treeHeight() / 2)
	case key.Matches(msg, m.keys.PageDown):
		m.move(m.treeHeight() / 2)
	case key.Matches(msg, m.keys.Top):
		m.move(-len(m.rows))
	case key.Matches(msg, m.keys.Bottom):
		m.move(len(m.rows))
	case key.Matches(msg, m.keys.Toggle):
		if r, ok := m.current(); ok && r.hasKids {
			m.expanded[r.key()] = !m.expanded[r.key()]
			m.rebuild(r.key())
		}
	case key.Matches(msg, m.keys.Right):
		if r, ok := m.current(); ok && r.hasKids {
			if !m.expanded[r.key()] {
				m.expanded[r.key()] = true
				m.rebuild(r.key())
			} else {
				m.move(1)
			}
		}
	case key.Matches(msg, m.keys.Left):
		if r, ok := m.current(); ok {
			if r.hasKids && m.expanded[r.key()] {
				m.expanded[r.key()] = false
				m.rebuild(r.key())
			} else if p := m.parentKey(r); p != "" {
				m.rebuild(p)
			}
		}
	case key.Matches(msg, m.keys.ExpandAll):
		keep := m.currentID()
		if m.board {
			for _, st := range boardOrder {
				m.expanded[groupKey(st)] = true
			}
		} else {
			for _, it := range m.items {
				m.expanded[it.ID] = true
			}
		}
		m.rebuild(keep)
	case key.Matches(msg, m.keys.CollapseAll):
		keep := m.currentID()
		for k := range m.expanded { // only this view's nodes
			if strings.HasPrefix(k, "status:") == m.board {
				delete(m.expanded, k)
			}
		}
		if r, ok := m.current(); ok && m.board {
			keep = m.parentKey(r)
		}
		// Land on the epic (or board section) that contained the cursor.
		for it := m.findItem(keep); it != nil; it = m.findItem(it.Parent) {
			keep = it.ID
		}
		m.rebuild(keep)
	}
}

func (m *Model) current() (row, bool) {
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		return m.rows[m.cursor], true
	}
	return row{}, false
}

func (m *Model) currentID() string {
	if r, ok := m.current(); ok {
		return r.key()
	}
	return ""
}

// parentKey is the row h/left jumps to: the parent item, or the ticket's
// section on the board. Empty for top-level rows.
func (m *Model) parentKey(r row) string {
	switch {
	case r.item == nil:
		return ""
	case m.board:
		return groupKey(r.item.Status)
	}
	return r.item.Parent
}

func (m *Model) findItem(id string) *model.Item {
	for _, it := range m.items {
		if it.ID == id {
			return it
		}
	}
	return nil
}

func (m *Model) move(delta int) {
	if len(m.rows) == 0 {
		return
	}
	m.cursor = max(0, min(m.cursor+delta, len(m.rows)-1))
	m.clampOffset()
	m.refreshPreview()
}

// scrollTree moves the tree viewport without changing the selection.
func (m *Model) scrollTree(delta int) {
	m.offset += delta
	m.freeTree = true
	m.clampOffset()
}

func (m *Model) clampOffset() {
	h := m.treeHeight()
	if h <= 0 {
		return
	}
	if m.freeTree {
		m.offset = max(0, min(m.offset, max(0, len(m.rows)-h)))
		return
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	m.offset = max(0, min(m.offset, max(0, len(m.rows)-h)))
}

// Layout ------------------------------------------------------------------

const (
	headerHeight = 1
	borderSize   = 2 // top+bottom or left+right
	padX         = 1
)

func (m *Model) helpKeys() help.KeyMap {
	switch {
	case m.find != nil:
		return finderHelp{}
	case m.ed.editing:
		return vimHelp(m.ed.vim.mode)
	case m.focus == focusPreview:
		return previewKeys{m.keys}
	case m.inbox:
		return inboxKeys{m.keys}
	}
	return m.keys
}

func (m *Model) footerHeight() int {
	return lipgloss.Height(m.footerView())
}

func (m *Model) bodyHeight() int {
	return max(3, m.height-headerHeight-m.footerHeight())
}

func (m *Model) treeWidth() int {
	return max(30, min(m.width*2/5, 64))
}

func (m *Model) treeHeight() int   { return m.bodyHeight() - borderSize }
func (m *Model) innerTreeW() int   { return m.treeWidth() - borderSize - 2*padX }
func (m *Model) previewWidth() int { return max(10, m.width-m.treeWidth()) }

func (m *Model) layout() {
	m.ed.resize(m.previewWidth()-borderSize-2*padX, m.bodyHeight()-borderSize)
	m.clampOffset()
}

// refreshPreview points the preview at the selected item (never mid-edit).
func (m *Model) refreshPreview() {
	if m.width == 0 || m.ed.editing {
		return
	}
	it := m.selected()
	if m.find != nil && it == nil { // searching, nothing matches
		m.ed.setDoc("", "_No matching items._", "", true)
		m.ed.tail = nil
		m.previewID = ""
		return
	}
	if r, ok := m.current(); ok && it == nil { // a board section
		m.ed.setDoc(m.groupDoc(r.group), "", "", r.key() != m.previewID)
		m.ed.tail = nil
		m.previewID = r.key()
		return
	}
	if it == nil {
		m.ed.setDoc("", "_Nothing here yet._\n\nCreate an epic from the shell: `shp new epic \"…\"`", "", true)
		m.ed.tail = nil
		m.previewID = ""
		return
	}
	var parent *model.Item
	if it.Parent != "" {
		parent = m.findItem(it.Parent)
	}
	pinned := render.PreviewHeader(it, parent)
	lead := ""
	if e, ok := worklog.LastReview(journal.ForItem(m.events, it.ID)); ok {
		lead = render.ReviewCard(it, e)
	}
	m.ed.setParts(pinned, lead, it.Body,
		render.Sections(it, m.items, store.Children(m.items, it.ID)), it.ID != m.previewID)
	m.ed.tail = worklog.RenderOptions(worklog.Build(journal.ForItem(m.events, it.ID)), m.ed.width, worklog.Options{
		ExpandAll:        m.expandLog,
		HideSubmitDetail: it.Status == model.Review || it.Status == model.AgentReview,
	})
	m.previewID = it.ID
}

// View --------------------------------------------------------------------

var (
	titleStyle                           = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "30", Dark: "45"})
	editStyle                            = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "136", Dark: "214"})
	errStyle                             = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "124", Dark: "203"})
	okStyle                              = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "28", Dark: "78"})
	normalBadge                          = lipgloss.NewStyle().Bold(true).Background(lipgloss.AdaptiveColor{Light: "37", Dark: "45"}).Foreground(lipgloss.AdaptiveColor{Light: "255", Dark: "0"})
	insertBadge                          = lipgloss.NewStyle().Bold(true).Background(lipgloss.AdaptiveColor{Light: "136", Dark: "214"}).Foreground(lipgloss.AdaptiveColor{Light: "255", Dark: "0"})
	focusedBorder lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "30", Dark: "45"}
	editBorder    lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "136", Dark: "214"}
	blurredBorder lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "250", Dark: "240"}
)

func (m *Model) View() string {
	if m.width == 0 {
		return ""
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		m.headerView(),
		lipgloss.JoinHorizontal(lipgloss.Top, m.treeView(), m.previewView()),
		m.footerView(),
	)
}

func (m *Model) headerView() string {
	left := titleStyle.Render("⛰ Sherpa") + faint.Render(" · ") + m.store.Config.Name + faint.Render(" ("+m.store.Config.Key+")")
	switch {
	case m.inbox:
		left += faint.Render(" · ") + titleStyle.Render("needs you")
	case m.board:
		left += faint.Render(" · ") + titleStyle.Render("board")
	}
	if n := m.needsYou(); n > 0 && !m.inbox && !m.ed.editing {
		word := "need"
		if n == 1 {
			word = "needs"
		}
		left += faint.Render(" · ") + handStyle.Render(fmt.Sprintf("✋ %d %s you (n)", n, word))
	}
	if w := m.workingView(); w != "" && !m.ed.editing {
		left += faint.Render(" · ") + w
	}
	if m.ed.editing {
		left += faint.Render(" · ") + editStyle.Render("editing "+m.editID)
		if m.ed.dirty {
			left += editStyle.Render(" ●")
		}
		v := m.ed.vim
		switch v.mode {
		case vimNormal:
			left += " " + normalBadge.Render(" NORMAL ")
			if p := v.pending(); p != "" {
				left += " " + p
			}
		case vimInsert:
			left += " " + insertBadge.Render(" INSERT ")
		case vimCommand:
			left += " " + normalBadge.Render(" COMMAND ") + " :" + v.cmd + "▏"
		}
	}
	counts := map[model.Status]int{}
	tickets := 0
	for _, it := range m.items {
		if it.Kind == model.Ticket {
			counts[it.Status]++
			tickets++
		}
	}
	summary := func(words bool) string {
		var parts []string
		for _, st := range model.Statuses {
			if n := counts[st]; n > 0 {
				label := fmt.Sprintf("%s %d", st.Glyph(), n)
				if words {
					label += " " + statusLabel(st)
				}
				parts = append(parts, render.StatusStyle(st).Render(label))
			}
		}
		out := strings.Join(parts, "  ")
		if tickets > 0 {
			out += faint.Render(fmt.Sprintf("  · %d%% done", counts[model.Done]*100/tickets))
		}
		return out
	}
	right := summary(true)
	if lipgloss.Width(left)+lipgloss.Width(right)+1 > m.width {
		right = summary(false)
	}
	switch {
	case m.flash != "" && strings.HasPrefix(m.flash, "⚠"):
		right = errStyle.Render(m.flash)
	case m.flash != "":
		right = okStyle.Render(m.flash)
	case m.err != nil:
		right = errStyle.Render("⚠ " + m.err.Error())
	}
	gap := max(1, m.width-lipgloss.Width(left)-lipgloss.Width(right))
	return left + strings.Repeat(" ", gap) + right
}

func (m *Model) panel(border lipgloss.TerminalColor, width int) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, padX).
		Width(width - borderSize).
		Height(m.bodyHeight() - borderSize)
}

func (m *Model) treeView() string {
	if m.find != nil {
		return m.finderView()
	}
	h, w := m.treeHeight(), m.innerTreeW()
	lines := make([]string, 0, h)
	if len(m.rows) == 0 {
		lines = append(lines, faint.Render("No items yet."))
	}
	for i := m.offset; i < len(m.rows) && len(lines) < h; i++ {
		r := m.rows[i]
		if r.item == nil {
			lines = append(lines, renderGroup(r, m.expanded[r.key()], i == m.cursor, m.focus == focusTree, w))
			continue
		}
		r.spin = m.spinner(r.item.ID)
		lines = append(lines, renderRow(r, m.expanded[r.key()], i == m.cursor, m.focus == focusTree, w, m.density))
	}
	border := blurredBorder
	if m.focus == focusTree {
		border = focusedBorder
	}
	return m.panel(border, m.treeWidth()).Render(strings.Join(lines, "\n"))
}

func (m *Model) previewView() string {
	border := blurredBorder
	switch {
	case m.ed.editing:
		border = editBorder
	case m.focus == focusPreview:
		border = focusedBorder
	}
	return m.panel(border, m.previewWidth()).Render(m.ed.View())
}
