package model

import (
	"regexp"
	"strconv"
	"strings"
)

// Structured ticket data. These live in frontmatter, not the markdown body,
// so agents update them through shp commands and never rewrite human prose;
// Sherpa renders them the same way everywhere.

// Check is one acceptance criterion. Waived counts as resolved, with a reason.
type Check struct {
	Text     string `yaml:"text" json:"text"`
	Done     bool   `yaml:"done,omitempty" json:"done,omitempty"`
	Evidence string `yaml:"evidence,omitempty" json:"evidence,omitempty"`
	Waived   string `yaml:"waived,omitempty" json:"waived,omitempty"`
}

func (c Check) Resolved() bool { return c.Done || c.Waived != "" }

// Measure is a measurable outcome: what was targeted and what was reached.
type Measure struct {
	Name     string `yaml:"name" json:"name"`
	Target   string `yaml:"target,omitempty" json:"target,omitempty"`
	Current  string `yaml:"current,omitempty" json:"current,omitempty"`
	Evidence string `yaml:"evidence,omitempty" json:"evidence,omitempty"`
}

// Met reports whether Current reaches Target. known is false when there is
// no target or the values can't be compared.
func (m Measure) Met() (met, known bool) {
	t, c := strings.TrimSpace(strings.ToLower(m.Target)), strings.TrimSpace(strings.ToLower(m.Current))
	if t == "" || c == "" {
		return false, false
	}
	if t == c {
		return true, true
	}
	tv, tok := quantity(t)
	cv, cok := quantity(c)
	if tok && cok {
		return cv >= tv, true
	}
	if yes(t) {
		return yes(c), true
	}
	return false, false
}

var reQuantity = regexp.MustCompile(`^[<>≥≤]?=?\s*(-?\d+(?:\.\d+)?)\s*(?:/\s*(\d+(?:\.\d+)?))?\s*%?`)

// quantity reads "2", "2/2", "95%", "≥ 3" as a number; "3/4" is 0.75 when a
// target is a ratio too, else its numerator.
func quantity(s string) (float64, bool) {
	m := reQuantity.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	n, _ := strconv.ParseFloat(m[1], 64)
	if m[2] != "" {
		d, _ := strconv.ParseFloat(m[2], 64)
		if d > 0 {
			return n / d, true
		}
	}
	return n, true
}

func yes(s string) bool {
	switch s {
	case "yes", "true", "y", "pass", "passed", "✓", "done":
		return true
	}
	return false
}

// Commit is one commit attributed to an item.
type Commit struct {
	SHA     string `yaml:"sha" json:"sha"`
	Subject string `yaml:"subject,omitempty" json:"subject,omitempty"`
}

// Links ties an item to the work that delivered it.
type Links struct {
	PR       string   `yaml:"pr,omitempty" json:"pr,omitempty"` // URL
	PRNumber int      `yaml:"pr_number,omitempty" json:"pr_number,omitempty"`
	PRState  string   `yaml:"pr_state,omitempty" json:"pr_state,omitempty"` // open, draft, merged, closed
	Branch   string   `yaml:"branch,omitempty" json:"branch,omitempty"`
	Commits  []Commit `yaml:"commits,omitempty" json:"commits,omitempty"`
	Added    int      `yaml:"added,omitempty" json:"added,omitempty"`
	Removed  int      `yaml:"removed,omitempty" json:"removed,omitempty"`
	Files    int      `yaml:"files,omitempty" json:"files,omitempty"`
	Related  []string `yaml:"related,omitempty" json:"related,omitempty"` // other PRs that contributed
}

func (l *Links) Empty() bool {
	return l == nil || (l.PR == "" && len(l.Commits) == 0 && len(l.Related) == 0)
}

// CheckCounts returns how many checks are resolved, out of how many.
func (it *Item) CheckCounts() (resolved, total int) {
	for _, c := range it.Checks {
		if c.Resolved() {
			resolved++
		}
	}
	return resolved, len(it.Checks)
}

// Body sections -----------------------------------------------------------------

var reHeading2 = regexp.MustCompile(`(?m)^## (.+?)[ \t]*$`)

// Section returns the text under "## <name>" (case-insensitive), or "" and false.
func Section(body, name string) (string, bool) {
	start, end, ok := sectionSpan(body, name)
	if !ok {
		return "", false
	}
	return strings.TrimSpace(body[start:end]), true
}

// sectionSpan finds the content span of "## name" up to the next "## ".
func sectionSpan(body, name string) (start, end int, ok bool) {
	heads := reHeading2.FindAllStringSubmatchIndex(body, -1)
	for i, h := range heads {
		if strings.EqualFold(strings.TrimSpace(body[h[2]:h[3]]), name) {
			end = len(body)
			if i+1 < len(heads) {
				end = heads[i+1][0]
			}
			return h[1], end, true
		}
	}
	return 0, 0, false
}

// SetSection replaces the content of "## name", or inserts the section after
// the body's first section (the goal or description) when it is missing.
func SetSection(body, name, content string) string {
	content = strings.TrimSpace(content)
	if start, end, ok := sectionSpan(body, name); ok {
		return strings.TrimRight(body[:start], " ") + "\n\n" + content + "\n\n" + strings.TrimLeft(body[end:], "\n")
	}
	block := "## " + name + "\n\n" + content + "\n\n"
	heads := reHeading2.FindAllStringIndex(body, -1)
	if len(heads) >= 2 {
		return body[:heads[1][0]] + block + body[heads[1][0]:]
	}
	return strings.TrimRight(body, "\n") + "\n\n" + strings.TrimRight(block, "\n") + "\n"
}

// RemoveSection deletes "## name" and its content.
func RemoveSection(body, name string) string {
	heads := reHeading2.FindAllStringSubmatchIndex(body, -1)
	for i, h := range heads {
		if strings.EqualFold(strings.TrimSpace(body[h[2]:h[3]]), name) {
			end := len(body)
			if i+1 < len(heads) {
				end = heads[i+1][0]
			}
			return strings.TrimRight(body[:h[0]], "\n") + "\n" + map[bool]string{true: "\n", false: ""}[end < len(body)] + body[end:]
		}
	}
	return body
}

var (
	reCheckItem = regexp.MustCompile(`^\s*[-*+]\s+\[([ xX])\]\s*(.*)$`)
	reBullet    = regexp.MustCompile(`^\s*(?:[-*+]|\d+[.)])\s+(.*)$`)
	reSentence  = regexp.MustCompile(`([.!?])\s+([A-Z(` + "`" + `])`)
)

// ExtractChecks turns an "Acceptance criteria" / "Acceptance" section into
// checks and returns the body without it. Checklists keep their ticks;
// bullets become one check each; prose is split into sentences. ok is false
// when there is no such section or it holds nothing but placeholders.
func ExtractChecks(body string) (checks []Check, rest string, ok bool) {
	for _, name := range []string{"Acceptance criteria", "Acceptance"} {
		text, found := Section(body, name)
		if !found {
			continue
		}
		var lines []string
		for _, l := range strings.Split(text, "\n") {
			if strings.TrimSpace(l) != "" {
				lines = append(lines, l)
			}
		}
		listy := len(lines) > 0
		for _, l := range lines {
			if !reCheckItem.MatchString(l) && !reBullet.MatchString(l) {
				listy = false
				break
			}
		}
		if listy {
			for _, l := range lines {
				if m := reCheckItem.FindStringSubmatch(l); m != nil {
					if t := strings.TrimSpace(m[2]); t != "" {
						checks = append(checks, Check{Text: t, Done: m[1] != " "})
					}
				} else if m := reBullet.FindStringSubmatch(l); m != nil {
					checks = append(checks, Check{Text: strings.TrimSpace(m[1])})
				}
			}
		} else if prose := strings.Join(strings.Fields(strings.Join(lines, " ")), " "); prose != "" {
			for _, s := range strings.Split(reSentence.ReplaceAllString(prose, "$1\n$2"), "\n") {
				if s = strings.TrimSpace(s); s != "" {
					checks = append(checks, Check{Text: s})
				}
			}
		}
		rest = RemoveSection(body, name)
		return checks, rest, true
	}
	return nil, body, false
}
