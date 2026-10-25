package cli

import (
	"strings"
	"testing"

	"github.com/RL-Charles/sherpa/internal/model"
)

func TestAgentClosesReadyTicket(t *testing.T) {
	s := project(t)
	it := get(t, s, "T-0001")
	it.Status = model.Review
	it.Deliverables = []model.Deliverable{{Text: "outcome", Done: true}}
	it.Checks = []model.Check{{Text: "unit: go test ./...", Done: true, Evidence: "abc1234"}}
	if err := s.Save(it); err != nil {
		t.Fatal(err)
	}
	mustShp(t, "agent:claude", "done", "T-0001")
	if got := get(t, s, "T-0001"); got.Status != model.Done {
		t.Fatalf("status = %s, want done", got.Status)
	}
}

func TestAgentCannotCloseIncompleteTicket(t *testing.T) {
	s := project(t)
	it := get(t, s, "T-0001")
	it.Status = model.Review
	if err := s.Save(it); err != nil {
		t.Fatal(err)
	}
	err := shp(t, "agent:claude", "done", "T-0001")
	if err == nil || !strings.Contains(err.Error(), "no deliverables") {
		t.Fatalf("incomplete ticket should refuse: %v", err)
	}
	if got := get(t, s, "T-0001"); got.Status != model.Review {
		t.Fatalf("refused close changed status to %s", got.Status)
	}
}

func TestOnlyHumansCloseStories(t *testing.T) {
	s := project(t)
	err := shp(t, "agent:claude", "done", "S-001")
	if err == nil || !strings.Contains(err.Error(), "human") {
		t.Fatalf("agent should not close a story: %v", err)
	}
	mustShp(t, "human:ann", "done", "S-001")
	if got := get(t, s, "S-001"); got.Status != model.Done {
		t.Fatalf("human should close the story, got %s", got.Status)
	}
}
