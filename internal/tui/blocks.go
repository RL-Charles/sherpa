package tui

import (
	"regexp"
	"strings"
)

// A block is a run of source lines [start, end) that renders as one unit:
// the live-preview editor shows the block under the cursor as raw text and
// every other block through Glamour.
//
// The splitter is line-based rather than AST-based so every block maps to
// exact source lines — goldmark's container nodes (lists, quotes) carry no
// source positions.
type block struct {
	start, end int
	kind       blockKind
}

type blockKind int

const (
	bParagraph blockKind = iota
	bHeading
	bList
	bFence
	bTable
	bQuote
	bRule
)

var (
	reHeading = regexp.MustCompile(`^ {0,3}#{1,6}(\s|$)`)
	reList    = regexp.MustCompile(`^(\s*)([-*+]|\d{1,9}[.)])(\s|$)`)
	reFence   = regexp.MustCompile("^ {0,3}(```|~~~)")
	reRule    = regexp.MustCompile(`^ {0,3}([-*_])(\s*[-*_]){2,}\s*$`)
	reSetext  = regexp.MustCompile(`^ {0,3}(=+|-+)\s*$`)
)

func isBlank(l string) bool { return strings.TrimSpace(l) == "" }
func isTable(l string) bool { return strings.HasPrefix(strings.TrimSpace(l), "|") }
func isQuote(l string) bool { return strings.HasPrefix(strings.TrimLeft(l, " "), ">") }

// interrupts reports whether l starts a new block, ending a paragraph or list item.
func interrupts(l string) bool {
	return reHeading.MatchString(l) || reFence.MatchString(l) || reRule.MatchString(l) ||
		reList.MatchString(l) || isTable(l) || isQuote(l)
}

// splitBlocks groups source lines into blocks. Blank lines belong to no block.
// Each list item is its own block so editing one item leaves its siblings rendered.
func splitBlocks(lines []string) []block {
	var out []block
	for i := 0; i < len(lines); {
		l := lines[i]
		j := i + 1
		kind := bParagraph
		switch {
		case isBlank(l):
			i++
			continue
		case reFence.MatchString(l):
			kind = bFence
			marker := reFence.FindStringSubmatch(l)[1]
			for j < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[j]), marker) {
				j++
			}
			if j < len(lines) {
				j++ // closing fence
			}
		case reHeading.MatchString(l):
			kind = bHeading
		case reRule.MatchString(l):
			kind = bRule
		case reList.MatchString(l):
			kind = bList
			for j < len(lines) && !isBlank(lines[j]) && !interrupts(lines[j]) {
				j++
			}
		case isTable(l):
			kind = bTable
			for j < len(lines) && isTable(lines[j]) {
				j++
			}
		case isQuote(l):
			kind = bQuote
			for j < len(lines) && !isBlank(lines[j]) && isQuote(lines[j]) {
				j++
			}
		default:
			for j < len(lines) && !isBlank(lines[j]) {
				if reSetext.MatchString(lines[j]) {
					j++ // "Title\n---" is a heading, not a paragraph + rule
					break
				}
				if interrupts(lines[j]) {
					break
				}
				j++
			}
		}
		out = append(out, block{start: i, end: j, kind: kind})
		i = j
	}
	return out
}

// isDiagram reports whether most lines of a paragraph are box-drawing art
// (├─ └─ │ ──▶ …), which markdown would otherwise reflow into one line.
func isDiagram(lines []string) bool {
	if len(lines) < 2 {
		return false
	}
	n := 0
	for _, l := range lines {
		if strings.ContainsAny(l, "├└│┌┐╭╮╰╯┬┴┼▶") || strings.Contains(l, "──") {
			n++
		}
	}
	return n*2 >= len(lines)
}
