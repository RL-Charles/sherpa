package model

import (
	"strings"
	"testing"
)

func TestMeasureMet(t *testing.T) {
	cases := []struct {
		target, current string
		met, known      bool
	}{
		{"2/2", "2/2", true, true}, {"2/2", "1/2", false, true}, {"2", "3", true, true},
		{"≥ 95%", "97%", true, true}, {"yes", "yes", true, true}, {"yes", "no", false, true},
		{"", "4", false, false}, {"fast", "slow", false, false},
	}
	for _, c := range cases {
		met, known := Measure{Target: c.target, Current: c.current}.Met()
		if met != c.met || known != c.known {
			t.Errorf("%q vs %q: got met=%v known=%v", c.current, c.target, met, known)
		}
	}
}

func TestExtractChecks(t *testing.T) {
	list := "## Description\n\nx\n\n## Acceptance criteria\n\n- [x] first\n- [ ] second\n- [ ] \n\n## Notes\n\nkeep\n"
	checks, rest, ok := ExtractChecks(list)
	if !ok || len(checks) != 2 || !checks[0].Done || checks[1].Done || checks[1].Text != "second" {
		t.Fatalf("checklist: %+v", checks)
	}
	if strings.Contains(rest, "Acceptance") || !strings.Contains(rest, "## Notes\n\nkeep") {
		t.Fatalf("section not removed cleanly:\n%s", rest)
	}
	prose := "## Goal\n\ng\n\n## Acceptance\n\nLogin succeeds with a valid token. An expired token is rejected, matching the spec.\n\n## Dependencies\n\nAPP-19\n"
	checks, rest, _ = ExtractChecks(prose)
	if len(checks) != 2 || checks[1].Text != "An expired token is rejected, matching the spec." {
		t.Fatalf("prose split: %+v", checks)
	}
	if !strings.Contains(rest, "## Dependencies") || strings.Contains(rest, "## Acceptance") {
		t.Fatalf("rest:\n%s", rest)
	}
	if _, _, ok := ExtractChecks("## Description\n\nnone\n"); ok {
		t.Fatal("no acceptance section: ok should be false")
	}
}

func TestSetSection(t *testing.T) {
	body := "## Goal\n\ng\n\n## Work\n\nw\n"
	got := SetSection(body, "Visual", "A ──▶ B")
	if got != "## Goal\n\ng\n\n## Visual\n\nA ──▶ B\n\n## Work\n\nw\n" {
		t.Fatalf("insert:\n%q", got)
	}
	if again := SetSection(got, "Visual", "C"); !strings.Contains(again, "## Visual\n\nC\n\n## Work") || strings.Count(again, "## Visual") != 1 {
		t.Fatalf("replace:\n%q", again)
	}
}

func TestSectionsIgnoreHeadingsInsideFences(t *testing.T) {
	body := "## Design\n\n~~~go\n// ## not a heading\ntype Status string\n~~~\n\n## Out of scope\n\n- none\n"
	design, ok := Section(body, "Design")
	if !ok || !strings.Contains(design, "// ## not a heading") || !strings.Contains(design, "type Status") {
		t.Fatalf("Design = %q, ok=%v", design, ok)
	}
	if strings.Contains(design, "Out of scope") {
		t.Fatalf("Design swallowed the next section:\n%s", design)
	}
	updated := SetSection(body, "Design", "~~~go\n// ## still code\n~~~")
	if !strings.Contains(updated, "## Out of scope") || !strings.Contains(updated, "// ## still code") {
		t.Fatalf("SetSection lost content:\n%s", updated)
	}
	removed := RemoveSection(updated, "Design")
	if strings.Contains(removed, "## Design") || strings.Contains(removed, "still code") || !strings.Contains(removed, "## Out of scope") {
		t.Fatalf("RemoveSection lost content:\n%s", removed)
	}
}
