package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/RL-Charles/sherpa/internal/model"
)

func mustShp(t *testing.T, actor string, args ...string) {
	t.Helper()
	if err := shp(t, actor, args...); err != nil {
		t.Fatalf("%v: %v", args, err)
	}
}

func findCode(it *model.Item, path string) *model.CodeRef {
	for i := range it.Code {
		if it.Code[i].Path == path {
			return &it.Code[i]
		}
	}
	return nil
}

func TestDeliverablesChecksAndCodeMap(t *testing.T) {
	s := project(t)
	mustShp(t, "agent:claude", "deliverable", "add", "Outcome A", "-t", "T-0001")
	mustShp(t, "agent:claude", "deliverable", "add", "Outcome B", "-t", "T-0001")
	mustShp(t, "agent:claude", "check", "add", "--for", "1", "unit: go test ./auth", "-t", "T-0001")
	mustShp(t, "agent:claude", "check", "add", "--for", "2", "e2e: two expiries", "-t", "T-0001")
	mustShp(t, "agent:claude", "where", "add", "internal/auth/token.go",
		"--symbols", "Rotate,Clock", "--deliverable", "1", "--note", "rotation", "-t", "T-0001")
	mustShp(t, "agent:claude", "where", "add", "api/auth/handlers.go",
		"--symbols", "handleRefresh", "--deliverable", "2", "-t", "T-0001")

	it := get(t, s, "T-0001")
	if len(it.Deliverables) != 2 || it.Deliverables[0].Text != "Outcome A" {
		t.Fatalf("deliverables = %+v", it.Deliverables)
	}
	if it.Checks[0].For != 1 || it.Checks[1].For != 2 {
		t.Fatalf("check links = %+v", it.Checks)
	}
	ref := findCode(it, "internal/auth/token.go")
	if ref == nil || ref.Deliverable != 1 || ref.Change != "modify" || strings.Join(ref.Symbols, ",") != "Rotate,Clock" {
		t.Fatalf("code ref = %+v", ref)
	}

	// Updating one field must not wipe the others.
	mustShp(t, "agent:claude", "where", "add", "internal/auth/token.go", "--note", "updated", "-t", "T-0001")
	it = get(t, s, "T-0001")
	ref = findCode(it, "internal/auth/token.go")
	if ref.Note != "updated" || ref.Deliverable != 1 || ref.Change != "modify" || strings.Join(ref.Symbols, ",") != "Rotate,Clock" {
		t.Fatalf("partial update lost fields: %+v", ref)
	}

	mustShp(t, "agent:claude", "where", "state", "internal/auth/token.go", "done", "-t", "T-0001")
	if ref := findCode(get(t, s, "T-0001"), "internal/auth/token.go"); ref.Status != "done" {
		t.Fatalf("state = %q, want done", ref.Status)
	}

	mustShp(t, "agent:claude", "deliverable", "done", "1", "-e", "token_test.go:TestRotateOnUse", "-t", "T-0001")
	it = get(t, s, "T-0001")
	if !it.Deliverables[0].Done || it.Deliverables[0].Evidence != "token_test.go:TestRotateOnUse" {
		t.Fatalf("deliverable done = %+v", it.Deliverables[0])
	}

	// Removing a deliverable unlinks its checks and shifts later links.
	mustShp(t, "agent:claude", "deliverable", "rm", "1", "-t", "T-0001")
	it = get(t, s, "T-0001")
	if len(it.Deliverables) != 1 || it.Deliverables[0].Text != "Outcome B" {
		t.Fatalf("deliverables after rm = %+v", it.Deliverables)
	}
	if it.Checks[0].For != 0 || it.Checks[1].For != 1 {
		t.Fatalf("check links after rm = %d, %d", it.Checks[0].For, it.Checks[1].For)
	}
	if ref := findCode(it, "internal/auth/token.go"); ref.Deliverable != 0 {
		t.Fatalf("code link after rm = %d, want 0", ref.Deliverable)
	}
	if ref := findCode(it, "api/auth/handlers.go"); ref.Deliverable != 1 {
		t.Fatalf("later code link after rm = %d, want 1", ref.Deliverable)
	}
}

func TestNewTicketTemplate(t *testing.T) {
	s := project(t)
	mustShp(t, "agent:claude", "new", "ticket", "Refresh tokens", "-p", "S-001",
		"--template", "feature", "-m", "Sessions die when the token expires.",
		"--deliverable", "Refresh accepts an expired token")
	it := get(t, s, "T-0002")
	if it.Template != "feature" || len(it.Deliverables) != 1 {
		t.Fatalf("template/deliverables = %q %+v", it.Template, it.Deliverables)
	}
	if why, ok := model.Section(it.Body, "Why"); !ok || !strings.Contains(why, "Sessions die") {
		t.Fatalf("description not in Why:\n%s", it.Body)
	}
	if _, ok := model.Section(it.Body, "Design"); !ok {
		t.Fatalf("feature template should have Design:\n%s", it.Body)
	}
}

func TestDesignSectionSetAndShow(t *testing.T) {
	s := project(t)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()
	go func() {
		_, _ = w.WriteString("## Design\n\n### Shape\n\nAdapter around the token source.\n\n### Types and enums\n\n~~~go\ntype Status string\n~~~\n")
		_ = w.Close()
	}()
	mustShp(t, "agent:claude", "design", "T-0001", "--stdin")
	it := get(t, s, "T-0001")
	design, ok := model.Section(it.Body, "Design")
	if !ok || !strings.Contains(design, "Adapter around") || !strings.Contains(design, "type Status string") {
		t.Fatalf("Design not set correctly:\n%s", it.Body)
	}
	if strings.Contains(design, "## Design") {
		t.Fatalf("pasted heading should be stripped:\n%s", design)
	}
}

func TestLintIsTemplateAware(t *testing.T) {
	base := &model.Item{
		ID: "T-0001", Kind: model.Ticket, Status: model.InProgress, Now: "working",
		Checks:       []model.Check{{Text: "unit: go test ./...", Done: true, Evidence: "abc1234", For: 1}},
		Code:         []model.CodeRef{{Path: "a.go", Deliverable: 1}},
		Deliverables: []model.Deliverable{{Text: "outcome", Done: true}},
	}
	bug := *base
	bug.Template = "bug"
	bug.Body = "## Expected\n\nexpected\n\n## Actual\n\nactual\n\n## Repro\n\n1. step\n\n## Design\n\ncause and test\n\n## Out of scope\n\nnot this\n"
	if got := lintItem(&bug); len(got) != 0 {
		t.Fatalf("a complete bug brief should be clean: %+v", got)
	}
	spike := *base
	spike.Template = "spike"
	spike.Deliverables = nil
	spike.Checks = nil
	spike.Code = nil
	spike.Body = "## Question\n\nq\n\n## Timebox\n\n1d\n\n## Context\n\nc\n\n## Findings\n\nf\n\n## Recommendation\n\nr\n\n## Out of scope\n\nn\n"
	if got := lintItem(&spike); len(got) != 0 {
		t.Fatalf("a complete spike brief should be clean: %+v", got)
	}
	missing := *base
	missing.Template = "bug"
	missing.Body = "## Expected\n\nexpected\n"
	if got := lintItem(&missing); len(got) == 0 {
		t.Fatal("an incomplete bug brief should warn")
	}
}
