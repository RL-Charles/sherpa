package model

import (
	"fmt"
	"strings"
)

// Epic goals are a bullet list of deliverables, at least MinGoals of them,
// each with real content: one line of prose hides what "done" means.

const MinGoals = 2

// Goals returns the filled-in bullets of a body's Goal section.
func Goals(body string) []string {
	sec, _ := Section(body, "Goal")
	var out []string
	for _, l := range strings.Split(sec, "\n") {
		m := reBullet.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		if t := strings.TrimSpace(m[1]); t != "" && !isPlaceholder(t) {
			out = append(out, t)
		}
	}
	return out
}

// isPlaceholder reports text that is only an italic hint, like "_Goal one_".
func isPlaceholder(s string) bool {
	return len(s) >= 2 && strings.HasPrefix(s, "_") && strings.HasSuffix(s, "_") && !strings.Contains(s[1:len(s)-1], "_")
}

// GoalProblem explains why an epic's goals fall short, or "" when they're
// fine (and always for stories and tickets).
func GoalProblem(it *Item) string {
	if it.Kind != Epic {
		return ""
	}
	if n := len(Goals(it.Body)); n < MinGoals {
		return fmt.Sprintf("%s's Goal needs at least %d bullet points with content (has %d)", it.ID, MinGoals, n)
	}
	return ""
}

// GoalList renders goals as markdown bullets.
func GoalList(goals []string) string {
	var b strings.Builder
	for _, g := range goals {
		b.WriteString("- " + strings.TrimSpace(g) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
