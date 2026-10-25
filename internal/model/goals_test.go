package model

import "testing"

func TestGoalProblem(t *testing.T) {
	for _, c := range []struct {
		body string
		ok   bool
	}{
		{"## Goal\n\nOne line of prose.\n", false},
		{"## Goal\n\n- Only one deliverable\n", false},
		{"## Goal\n\n- Ship it\n-   \n", false},
		{DefaultBody(Epic, ""), false}, // placeholders don't count
		{"## Goal\n\n- Ship the CLI\n- Ship the TUI\n\n## Notes\n\n- not a goal\n", true},
		{"## Goal\n\nIntro.\n\n1. Ship the CLI\n2. Ship the TUI\n", true},
	} {
		got := GoalProblem(&Item{ID: "E-001", Kind: Epic, Body: c.body}) == ""
		if got != c.ok {
			t.Errorf("ok=%v, want %v for %q", got, c.ok, c.body)
		}
	}
	if p := GoalProblem(&Item{Kind: Story, Body: "## Description\n\nx\n"}); p != "" {
		t.Errorf("stories have no goal rule, got %q", p)
	}
}
