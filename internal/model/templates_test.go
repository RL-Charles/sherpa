package model

import (
	"strings"
	"testing"
)

func TestDeliverableAndCheckMapping(t *testing.T) {
	it := &Item{
		Deliverables: []Deliverable{
			{Text: "one", Done: true},
			{Text: "two"},
		},
		Checks: []Check{
			{Text: "a", Done: true, For: 1},
			{Text: "b", For: 1},
			{Text: "c", Done: true, For: 2},
			{Text: "unlinked"},
			{Text: "bad link", For: 9},
		},
	}
	if done, total := it.DeliverableCounts(); done != 1 || total != 2 {
		t.Errorf("DeliverableCounts = %d/%d, want 1/2", done, total)
	}
	if r, total := it.CheckCountsFor(1); r != 1 || total != 2 {
		t.Errorf("CheckCountsFor(1) = %d/%d, want 1/2", r, total)
	}
	if got := it.ChecksFor(2); len(got) != 1 || got[0].Text != "c" {
		t.Errorf("ChecksFor(2) = %+v", got)
	}
	unlinked := it.UnlinkedChecks()
	if len(unlinked) != 2 || unlinked[0].Text != "unlinked" || unlinked[1].Text != "bad link" {
		t.Errorf("UnlinkedChecks = %+v", unlinked)
	}
}

func TestTemplateBodies(t *testing.T) {
	for _, name := range TemplateNames {
		body, err := TemplateBody(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !strings.HasPrefix(body, "## ") {
			t.Errorf("%s template should start with a section: %q", name, body[:20])
		}
	}
	if _, err := TemplateBody("nope"); err == nil {
		t.Fatal("unknown template should error")
	}
	body, _ := TemplateBody("feature")
	filled := FillTemplate(body, "Sessions die when the token expires.")
	if !strings.Contains(filled, "Sessions die when the token expires.") {
		t.Fatalf("description not inserted:\n%s", filled[:120])
	}
	if strings.Contains(filled, "_What hurts today") {
		t.Fatal("first placeholder should be replaced")
	}
}
