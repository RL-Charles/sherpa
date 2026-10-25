package workflow

import (
	"strings"
	"testing"

	"github.com/RL-Charles/sherpa/internal/model"
)

func readyTicket(id string, status model.Status) *model.Item {
	return &model.Item{
		ID: id, Kind: model.Ticket, Status: status,
		Deliverables: []model.Deliverable{{Text: "outcome", Done: true}},
		Checks:       []model.Check{{Text: "unit: go test ./...", Done: true, Evidence: "abc1234"}},
	}
}

func TestAgentsCannotSetDoneDirectly(t *testing.T) {
	agent := Actor{Kind: Agent, Name: "claude"}
	human := Actor{Kind: Human, Name: "charles"}
	it := readyTicket("T-0001", model.InProgress)

	if err := CheckTransition(agent, it, model.Done); err == nil || !strings.Contains(err.Error(), "shp done") {
		t.Fatalf("agent direct done should point at shp done: %v", err)
	}
	if err := CheckTransition(agent, it, model.Review); err != nil {
		t.Fatalf("agent should submit for review: %v", err)
	}
	if err := CheckTransition(human, it, model.Done); err != nil {
		t.Fatalf("human should mark done: %v", err)
	}
	done := readyTicket("T-0002", model.Done)
	if err := CheckTransition(agent, done, model.InProgress); err == nil {
		t.Fatal("agent should not reopen done work")
	}
}

func TestAgentsCloseReadyTickets(t *testing.T) {
	agent := Actor{Kind: Agent, Name: "claude"}
	for _, st := range []model.Status{model.Review, model.AgentReview} {
		if err := CheckDone(agent, readyTicket("T-0001", st)); err != nil {
			t.Fatalf("agent should close a ready ticket in %s: %v", st, err)
		}
	}
	for _, c := range []struct {
		name string
		it   *model.Item
		want string
	}{
		{"in progress", readyTicket("T-0001", model.InProgress), "submit it first"},
		{"no deliverables", &model.Item{ID: "T-1", Kind: model.Ticket, Status: model.Review,
			Checks: []model.Check{{Text: "x", Done: true}}}, "no deliverables"},
		{"no checks", &model.Item{ID: "T-1", Kind: model.Ticket, Status: model.Review,
			Deliverables: []model.Deliverable{{Text: "x", Done: true}}}, "no checks"},
		{"open deliverable", &model.Item{ID: "T-1", Kind: model.Ticket, Status: model.Review,
			Deliverables: []model.Deliverable{{Text: "x"}},
			Checks:       []model.Check{{Text: "y", Done: true}}}, "not ready"},
		{"open check", &model.Item{ID: "T-1", Kind: model.Ticket, Status: model.Review,
			Deliverables: []model.Deliverable{{Text: "x", Done: true}},
			Checks:       []model.Check{{Text: "y"}}}, "not ready"},
		{"manual check", &model.Item{ID: "T-1", Kind: model.Ticket, Status: model.Review,
			Deliverables: []model.Deliverable{{Text: "x", Done: true}},
			Checks:       []model.Check{{Text: "y", Manual: true, How: "look"}}}, "not ready"},
	} {
		err := CheckDone(agent, c.it)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want %q", c.name, err, c.want)
		}
	}
}

func TestStoriesAndEpicsAreHumanOnly(t *testing.T) {
	agent := Actor{Kind: Agent, Name: "claude"}
	human := Actor{Kind: Human, Name: "charles"}
	for _, kind := range []model.Kind{model.Story, model.Epic} {
		it := readyTicket("S-001", model.Review)
		it.Kind = kind
		if err := CheckDone(agent, it); err == nil || !strings.Contains(err.Error(), "human") {
			t.Errorf("agent should not close a %s: %v", kind, err)
		}
		if err := CheckDone(human, it); err != nil {
			t.Errorf("human should close a %s: %v", kind, err)
		}
	}
}

func TestHumansCloseWithoutCriteria(t *testing.T) {
	human := Actor{Kind: Human, Name: "charles"}
	if err := CheckDone(human, &model.Item{ID: "T-1", Kind: model.Ticket, Status: model.InProgress}); err != nil {
		t.Fatalf("human should close an old ticket without a brief: %v", err)
	}
}

func TestCurrentActorEnv(t *testing.T) {
	t.Setenv("SHERPA_ACTOR", "agent:codex")
	if a := CurrentActor(); a.Kind != Agent || a.Name != "codex" {
		t.Fatalf("got %v", a)
	}
	t.Setenv("SHERPA_ACTOR", "")
	t.Setenv("CLAUDECODE", "1")
	if a := CurrentActor(); a.Kind != Agent || a.Name != "claude" {
		t.Fatalf("got %v", a)
	}
}
