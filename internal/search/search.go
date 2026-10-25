// Package search finds items by fuzzy-matching their ID, title, labels,
// assignee, deliverables and code map, then by plain text in their bodies.
//
//	oauth lgn          fuzzy: letters in order, gaps allowed
//	30, t30, T-0030    an item number jumps straight to that item
//	is:ready is:story  filter by status or kind
//	@claude            filter by assignee
package search

import (
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/RL-Charles/sherpa/internal/model"
)

// Result is one matching item. Hits are rune positions in Item.Title to
// highlight; Snippet is set for matches found in the body text.
type Result struct {
	Item    *model.Item
	Score   int
	Hits    []int
	Snippet string
}

// Fuzzy reports whether pattern's characters appear in text in order
// (case-insensitively), with a score that rewards consecutive runs, word
// starts and early matches, and the matched rune positions.
func Fuzzy(pattern, text string) (score int, hits []int, ok bool) {
	p := []rune(strings.ToLower(pattern))
	t := []rune(strings.ToLower(text))
	if len(p) == 0 {
		return 0, nil, true
	}
	// Forward pass finds where the match can end; a backward pass from
	// there finds the tightest window, like fzf's v1 algorithm.
	pi, end := 0, -1
	for i := 0; i < len(t) && pi < len(p); i++ {
		if t[i] == p[pi] {
			pi++
			if pi == len(p) {
				end = i
			}
		}
	}
	if end < 0 {
		return 0, nil, false
	}
	pi = len(p) - 1
	start := end
	for i := end; i >= 0 && pi >= 0; i-- {
		if t[i] == p[pi] {
			pi--
			start = i
		}
	}
	pi = 0
	prev := -2
	for i := start; i <= end && pi < len(p); i++ {
		if t[i] != p[pi] {
			continue
		}
		s := 16
		if i == prev+1 {
			s += 12 // consecutive
		}
		if i == 0 || isBoundary(t[i-1]) {
			s += 10 // start of a word
		}
		score += s
		hits = append(hits, i)
		prev = i
		pi++
	}
	score -= (end - start + 1) - len(p) // gaps inside the match
	score -= start / 4                  // later matches rank a little lower
	return score, hits, true
}

func isBoundary(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }

type query struct {
	words    []string
	statuses map[model.Status]bool
	kinds    map[model.Kind]bool
	assignee string
	id       string // a word that names an item exactly
}

func parse(q string) query {
	out := query{statuses: map[model.Status]bool{}, kinds: map[model.Kind]bool{}}
	for _, w := range strings.Fields(q) {
		lw := strings.ToLower(w)
		switch {
		case strings.HasPrefix(lw, "is:"):
			v := strings.TrimPrefix(lw, "is:")
			if st, err := model.ParseStatus(v); err == nil {
				out.statuses[st] = true
			} else if k, err := model.ParseKind(v); err == nil {
				out.kinds[k] = true
			}
		case strings.HasPrefix(w, "@") && len(w) > 1:
			out.assignee = strings.ToLower(w[1:])
		default:
			if id, ok := asID(w); ok {
				out.id = id
			}
			out.words = append(out.words, w)
		}
	}
	return out
}

// asID reads "T-0030", "t30" or a bare "30" as an item ID (bare numbers
// mean tickets).
func asID(w string) (string, bool) {
	if n, err := strconv.Atoi(w); err == nil && n > 0 {
		return model.FormatID(model.Ticket, n), true
	}
	if id, _, _, err := model.ParseID(w); err == nil {
		return id, true
	}
	return "", false
}

func haystack(it *model.Item) string {
	parts := []string{it.ID, it.Title}
	parts = append(parts, it.Labels...)
	if it.Assignee != "" {
		parts = append(parts, "@"+it.Assignee)
	}
	for _, d := range it.Deliverables {
		parts = append(parts, d.Text)
	}
	for _, c := range it.Code {
		parts = append(parts, c.Path)
		parts = append(parts, c.Symbols...)
	}
	return strings.Join(parts, " ")
}

// searchableText is what body search scans: prose plus the structured facts a
// reader might search for (a symbol, a file, a check).
func searchableText(it *model.Item) string {
	var b strings.Builder
	b.WriteString(it.Body)
	for _, d := range it.Deliverables {
		b.WriteString("\n" + d.Text + " " + d.Evidence + " " + d.Note)
	}
	for _, c := range it.Code {
		b.WriteString("\n" + c.Path + " " + strings.Join(c.Symbols, " ") + " " + c.Note)
	}
	for _, c := range it.Checks {
		b.WriteString("\n" + c.Text + " " + c.Evidence + " " + c.How)
	}
	return b.String()
}

// Search ranks items for q. Title matches come first, then items whose body
// contains every word. limit <= 0 means no limit.
func Search(items []*model.Item, q string, limit int) []Result {
	qq := parse(q)
	var titled, texted []Result
	for _, it := range items {
		if len(qq.statuses) > 0 && !qq.statuses[it.Status] {
			continue
		}
		if len(qq.kinds) > 0 && !qq.kinds[it.Kind] {
			continue
		}
		if qq.assignee != "" && !strings.Contains(strings.ToLower(it.Assignee), qq.assignee) {
			continue
		}
		if len(qq.words) == 0 {
			titled = append(titled, Result{Item: it})
			continue
		}
		if qq.id != "" && it.ID == qq.id {
			titled = append(titled, Result{Item: it, Score: 1 << 20})
			continue
		}
		total, ok := 0, true
		var hits []int
		for _, w := range qq.words {
			s, _, matched := Fuzzy(w, haystack(it))
			if !matched {
				ok = false
				break
			}
			total += s
			if _, h, tm := Fuzzy(w, it.Title); tm {
				hits = append(hits, h...)
			}
		}
		if ok {
			titled = append(titled, Result{Item: it, Score: total, Hits: dedupe(hits)})
			continue
		}
		if snip, ok := inText(searchableText(it), qq.words); ok {
			texted = append(texted, Result{Item: it, Snippet: snip})
		}
	}
	sort.SliceStable(titled, func(i, j int) bool { return titled[i].Score > titled[j].Score })
	out := append(titled, texted...)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// inText reports whether body contains every word and returns a one-line
// snippet around the first.
func inText(body string, words []string) (string, bool) {
	low := strings.ToLower(body)
	first := -1
	for _, w := range words {
		i := strings.Index(low, strings.ToLower(w))
		if i < 0 {
			return "", false
		}
		if first < 0 || i < first {
			first = i
		}
	}
	r := []rune(body)
	at := len([]rune(body[:first]))
	from, to := max(0, at-30), min(len(r), at+50)
	snip := strings.Join(strings.Fields(string(r[from:to])), " ")
	if from > 0 {
		snip = "…" + snip
	}
	if to < len(r) {
		snip += "…"
	}
	return snip, true
}

func dedupe(xs []int) []int {
	sort.Ints(xs)
	var out []int
	for i, x := range xs {
		if i == 0 || x != xs[i-1] {
			out = append(out, x)
		}
	}
	return out
}
