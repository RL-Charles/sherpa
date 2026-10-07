package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestRenderTableBoxed(t *testing.T) {
	src := []string{
		"| Item | What to check |",
		"|---|:---:|",
		"| **E-002** | `code` and _italic_ |",
		"| T-0021 | pipe \\| inside |",
		"| T-0008 | `` `x` `` |",
	}
	lines, ok := renderTable(src, 80)
	if !ok {
		t.Fatal("well-formed table rejected")
	}
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = ansi.Strip(l)
	}
	got := strings.Join(plain, "\n")
	for _, want := range []string{"╭", "╮", "╰", "╯", "├", "┼", "E-002", "code and italic", "pipe | inside", "`x`"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "**") || strings.Contains(got, "``") {
		t.Errorf("inline markers should be rendered, not shown:\n%s", got)
	}
	// header, separator, 3 rows, 2 row separators, top + bottom border
	if len(lines) != 9 {
		t.Errorf("want 9 lines (boxed with row separators), got %d:\n%s", len(lines), got)
	}
}

func TestRenderTableWrapsWhenNarrow(t *testing.T) {
	src := []string{
		"| Option | Verdict |",
		"|---|---|",
		"| Sequential numbering with a suffix | a fairly long verdict that must wrap |",
	}
	lines, _ := renderTable(src, 40)
	for _, l := range lines {
		if w := ansi.StringWidth(l); w > 40 {
			t.Fatalf("line %d wide exceeds 40: %q", w, ansi.Strip(l))
		}
	}
	joined := ansi.Strip(strings.Join(lines, " "))
	for _, word := range []string{"suffix", "wrap"} {
		if !strings.Contains(joined, word) {
			t.Fatalf("wrapped table lost %q:\n%s", word, joined)
		}
	}
}

func TestRenderTableRejectsNonTable(t *testing.T) {
	if _, ok := renderTable([]string{"| just a line |", "| another |"}, 80); ok {
		t.Fatal("rows without a separator line are not a table")
	}
}

func TestRenderTableKeepsWordsWhole(t *testing.T) {
	src := []string{
		"| View | Key | Status | Ticket |",
		"|---|---|---|---|",
		"| Tree + preview | default | done | T-0006 |",
		"| Live-preview editor | e | done | T-0023 |",
		"| Kanban board | b | planned | T-0009 |",
	}
	check := func(width int, whole []string) {
		t.Helper()
		lines, _ := renderTable(src, width)
		text := ansi.Strip(strings.Join(lines, "\n"))
		for _, l := range lines {
			if w := ansi.StringWidth(l); w > width {
				t.Fatalf("line wider than %d (%d): %q", width, w, ansi.Strip(l))
			}
		}
		for _, word := range whole {
			if !strings.Contains(text, word) {
				t.Fatalf("width %d: word %q was broken:\n%s", width, word, text)
			}
		}
	}
	check(50, []string{"default", "planned", "T-0006", "Live-preview"}) // room for every word
	check(44, []string{"default", "planned", "T-0006"})                 // only the longest word may break
}
