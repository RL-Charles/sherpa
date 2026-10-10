package tui

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Up, Down, Left, Right  key.Binding
	Toggle, Top, Bottom    key.Binding
	PageUp, PageDown       key.Binding
	ExpandAll, CollapseAll key.Binding
	Focus, Reload, Edit    key.Binding
	Worklog, Status, Board key.Binding
	Help, Quit             key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		Up:          key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:        key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Left:        key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "collapse/parent")),
		Right:       key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "expand/child")),
		Toggle:      key.NewBinding(key.WithKeys("enter", " "), key.WithHelp("enter", "toggle")),
		Top:         key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g", "top")),
		Bottom:      key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G", "bottom")),
		PageUp:      key.NewBinding(key.WithKeys("pgup", "ctrl+u"), key.WithHelp("ctrl+u", "page up")),
		PageDown:    key.NewBinding(key.WithKeys("pgdown", "ctrl+d"), key.WithHelp("ctrl+d", "page down")),
		ExpandAll:   key.NewBinding(key.WithKeys("E"), key.WithHelp("E", "expand all")),
		CollapseAll: key.NewBinding(key.WithKeys("C"), key.WithHelp("C", "collapse all")),
		Focus:       key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch pane")),
		Reload:      key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reload")),
		Edit:        key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
		Worklog:     key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "all worklog sessions")),
		Status:      key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "set status")),
		Board:       key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "board/tree")),
		Help:        key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:        key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Toggle, k.Edit, k.Status, k.Board, k.Focus, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Left, k.Right, k.Toggle},
		{k.Top, k.Bottom, k.PageUp, k.PageDown},
		{k.ExpandAll, k.CollapseAll, k.Focus, k.Reload, k.Edit, k.Status, k.Board, k.Worklog},
		{k.Help, k.Quit},
	}
}

// previewKeys is the help shown while the preview pane has focus.
type previewKeys struct{ k keyMap }

func (p previewKeys) ShortHelp() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("j"), key.WithHelp("↑↓/jk", "scroll")),
		key.NewBinding(key.WithKeys("e"), key.WithHelp("e/enter", "edit")),
		p.k.Status,
		p.k.Worklog,
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc/tab", "back to tree")),
		p.k.Help, p.k.Quit,
	}
}

func (p previewKeys) FullHelp() [][]key.Binding { return [][]key.Binding{p.ShortHelp()} }

// editKeyMap is display-only help for edit mode; keys are handled by the vim engine.
type editKeyMap []key.Binding

func (e editKeyMap) ShortHelp() []key.Binding  { return e }
func (e editKeyMap) FullHelp() [][]key.Binding { return [][]key.Binding{e} }

func hint(keys, desc string) key.Binding {
	return key.NewBinding(key.WithKeys(keys), key.WithHelp(keys, desc))
}

var (
	normalHelp = editKeyMap{
		hint("i a o", "insert"), hint(":w", "save"), hint(":wq", "save & close"), hint(":q!", "discard"),
		hint("u ^r", "undo/redo"), hint("dd yy p", "cut/copy/paste"), hint("ciw", "change word"), hint("^x", "toggle [ ]"),
	}
	insertHelp = editKeyMap{
		hint("esc", "normal mode"), hint("enter", "continues lists"), hint("tab", "indent"),
		hint("^x", "toggle [ ]"), hint("^w", "delete word"), hint("^s", "save"),
	}
	commandHelp = editKeyMap{
		hint("enter", "run"), hint("esc", "cancel"), hint(":w :wq :q :q!", "write/quit"), hint(":N", "go to line"),
	}
)

func vimHelp(mode vimMode) editKeyMap {
	switch mode {
	case vimInsert:
		return insertHelp
	case vimCommand:
		return commandHelp
	}
	return normalHelp
}
