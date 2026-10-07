package workflow

import (
	"testing"

	"github.com/RL-Charles/sherpa/internal/model"
)

func TestAgentsCannotFinish(t *testing.T) {
	agent := Actor{Kind: Agent, Name: "claude"}
	human := Actor{Kind: Human, Name: "charles"}
	it := &model.Item{ID: "T-0001", Status: model.InProgress}

	if err := CheckTransition(agent, it, model.Done); err == nil {
		t.Fatal("agent should not be able to mark done")
	}
	if err := CheckTransition(agent, it, model.Review); err != nil {
		t.Fatalf("agent should submit for review: %v", err)
	}
	if err := CheckTransition(human, it, model.Done); err != nil {
		t.Fatalf("human should mark done: %v", err)
	}
	done := &model.Item{ID: "T-0002", Status: model.Done}
	if err := CheckTransition(agent, done, model.InProgress); err == nil {
		t.Fatal("agent should not reopen done work")
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
