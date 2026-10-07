package tui

import (
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/RL-Charles/sherpa/internal/journal"
)

func typeText(e *editor, s string) {
	for _, r := range s {
		if r == ' ' {
			e.key(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
			continue
		}
		e.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func keyT(e *editor, t tea.KeyType) { e.key(tea.KeyMsg{Type: t}) }

func editorWith(body string) *editor {
	e := newEditor("dark")
	e.resize(80, 30)
	e.setDoc("", body, "", true)
	e.startEditing()
	return e
}

func TestEditorListContinuation(t *testing.T) {
	e := editorWith("- [x] first")
	keyT(e, tea.KeyEnd)
	keyT(e, tea.KeyEnter)
	typeText(e, "second")
	keyT(e, tea.KeyEnter)
	keyT(e, tea.KeyEnter) // empty item → leaves the list
	typeText(e, "after")
	want := "- [x] first\n- [ ] second\nafter\n"
	if got := e.body(); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestEditorOrderedListAndIndent(t *testing.T) {
	e := editorWith("1. one")
	keyT(e, tea.KeyEnd)
	keyT(e, tea.KeyEnter)
	typeText(e, "two")
	keyT(e, tea.KeyTab)
	if got := e.body(); got != "1. one\n  2. two\n" {
		t.Fatalf("got %q", got)
	}
	keyT(e, tea.KeyShiftTab)
	if got := e.body(); got != "1. one\n2. two\n" {
		t.Fatalf("outdent: got %q", got)
	}
}

func TestEditorBackspaceJoinAndUndo(t *testing.T) {
	e := editorWith("hello\nworld")
	keyT(e, tea.KeyDown)
	keyT(e, tea.KeyHome)
	keyT(e, tea.KeyBackspace)
	if got := e.body(); got != "helloworld\n" || e.col != 5 {
		t.Fatalf("join: got %q col %d", got, e.col)
	}
	typeText(e, " big")
	keyT(e, tea.KeyCtrlZ) // undo "big"
	keyT(e, tea.KeyCtrlZ) // undo " "
	keyT(e, tea.KeyCtrlZ) // undo the join
	if got := e.body(); got != "hello\nworld\n" {
		t.Fatalf("undo: got %q", got)
	}
}

func TestEditorToggleCheckbox(t *testing.T) {
	e := editorWith("- [ ] task")
	keyT(e, tea.KeyCtrlX)
	if got := e.body(); got != "- [x] task\n" {
		t.Fatalf("got %q", got)
	}
}

func TestLivePreviewRawOnlyUnderCursor(t *testing.T) {
	e := editorWith("## Heading\n\nSome **bold** text\n\n- [ ] item")
	view := ansi.Strip(e.View())
	if !strings.Contains(view, "▎ ## Heading") {
		t.Fatalf("cursor block should be raw with gutter:\n%s", view)
	}
	if strings.Contains(view, "**bold**") {
		t.Fatalf("non-cursor paragraph should be rendered, not raw:\n%s", view)
	}
	keyT(e, tea.KeyDown)
	keyT(e, tea.KeyDown)
	view = ansi.Strip(e.View())
	if !strings.Contains(view, "▎ Some **bold** text") || strings.Contains(view, "▎ ## Heading") {
		t.Fatalf("raw block should follow the cursor:\n%s", view)
	}
}

func TestEditSaveAndConflict(t *testing.T) {
	m := fixture(t)
	press(m, "j", "j") // T-0001
	mvim(m, "e")
	if !m.ed.editing || m.editID != "T-0001" || m.ed.vim.mode != vimNormal {
		t.Fatalf("e should start editing T-0001 in normal mode (editing=%v id=%s)", m.ed.editing, m.editID)
	}
	mvim(m, "iqjk<esc>:wq<cr>") // letters are text in insert mode, not commands
	if m.ed.editing {
		t.Fatal(":wq should save and leave edit mode")
	}
	it, _ := m.store.Get("T-0001")
	if !strings.HasPrefix(it.Body, "qjk") {
		t.Fatalf("edit not saved, body starts %q", it.Body[:10])
	}
	events, _ := journal.ReadAll(m.store.Dir)
	if last := events[len(events)-1]; last.Type != "edit" || last.Item != "T-0001" {
		t.Fatalf("edit not journalled: %+v", last)
	}

	// An agent rewrites part of the body while we edit → save must refuse.
	mvim(m, "eiX<esc>")
	agent, _ := m.store.Get("T-0001")
	agent.Body = strings.Replace(agent.Body, "about Ticket one", "rewritten by agent", 1)
	agent.Status = "review"
	if err := m.store.Save(agent); err != nil {
		t.Fatal(err)
	}
	m.Update(tickMsg{}) // reload is paused while editing
	if !m.ed.editing {
		t.Fatal("tick should not kick us out of edit mode")
	}
	mvim(m, ":wq<cr>")
	if !m.conflict || !m.ed.editing {
		t.Fatal(":wq should refuse to save over an agent's change")
	}
	mvim(m, ":wq!<cr>")
	got, _ := m.store.Get("T-0001")
	if m.ed.editing || !strings.HasPrefix(got.Body, "Xqjk") || got.Status != "review" {
		t.Fatalf("forced save should write body but keep agent's status; status=%s body=%q", got.Status, got.Body[:10])
	}
}

func TestDiscardEdit(t *testing.T) {
	m := fixture(t)
	press(m, "j", "j")
	before, _ := m.store.Get("T-0001")
	mvim(m, "eijunk<esc>:q<cr>")
	if !m.ed.editing || !strings.Contains(m.flash, "E37") {
		t.Fatalf(":q with changes should refuse (flash=%q)", m.flash)
	}
	mvim(m, ":q!<cr>")
	after, _ := m.store.Get("T-0001")
	if m.ed.editing || after.Body != before.Body {
		t.Fatal(":q! should discard and leave edit mode without writing")
	}
}

func TestSaveMergesAppendedWorklog(t *testing.T) {
	m := fixture(t)
	press(m, "j", "j")
	mvim(m, "eiMine <esc>")
	agent, _ := m.store.Get("T-0001")
	agent.Body += "\nagent was here\n"
	if err := m.store.Save(agent); err != nil {
		t.Fatal(err)
	}
	mvim(m, "ZZ")
	if m.conflict || m.ed.editing {
		t.Fatalf("appended worklog should merge cleanly (conflict=%v)", m.conflict)
	}
	got, _ := m.store.Get("T-0001")
	if !strings.HasPrefix(got.Body, "Mine ") || !strings.Contains(got.Body, "agent was here") {
		t.Fatalf("merge lost something:\n%s", got.Body)
	}
}

var vimTokens = map[string]tea.KeyMsg{
	"esc": {Type: tea.KeyEsc}, "cr": {Type: tea.KeyEnter}, "bs": {Type: tea.KeyBackspace},
	"c-r": {Type: tea.KeyCtrlR}, "c-x": {Type: tea.KeyCtrlX}, "c-w": {Type: tea.KeyCtrlW}, "c-s": {Type: tea.KeyCtrlS},
}

// keys turns "dw<esc>:wq<cr>" into key messages.
func keys(seq string) []tea.KeyMsg {
	var out []tea.KeyMsg
	for len(seq) > 0 {
		if strings.HasPrefix(seq, "<") {
			if end := strings.Index(seq, ">"); end > 0 {
				if k, ok := vimTokens[seq[1:end]]; ok {
					out = append(out, k)
					seq = seq[end+1:]
					continue
				}
			}
		}
		r, size := utf8.DecodeRuneInString(seq)
		seq = seq[size:]
		if r == ' ' {
			out = append(out, tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
		} else {
			out = append(out, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
	}
	return out
}

func mvim(m *Model, seq string) {
	for _, k := range keys(seq) {
		m.Update(k)
	}
}

func TestWideTableNotTruncated(t *testing.T) {
	md := "| Option | Example | Collision-free | Readable | Verdict |\n|---|---|---|---|---|\n" +
		"| Sequential (today) | `T-0031` | ✗ | ✓ | keep for solo |\n| Sequential + suffix | `T-0031-k3` | ✓ | ~ | candidate |\n" +
		"| ULID | `T-01J9Z…` | ✓ | ✗ | no |\n| Renumber on merge | `shp fix-ids` | ✓ | ✓ | candidate |"
	e := newEditor("dark")
	e.resize(86, 40)
	e.setDoc("", md, "", true)
	view := ansi.Strip(e.View())
	if !strings.Contains(view, "keep for solo") {
		t.Fatalf("table cell truncated:\n%s", view)
	}
	for _, l := range strings.Split(view, "\n") {
		if ansi.StringWidth(l) > 86 {
			t.Fatalf("line wider than pane: %q", l)
		}
	}
}
