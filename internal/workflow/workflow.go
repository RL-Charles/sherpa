// Package workflow decides who may move an item to which status.
package workflow

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/RL-Charles/sherpa/internal/model"
)

type ActorKind string

const (
	Human ActorKind = "human"
	Agent ActorKind = "agent"
)

type Actor struct {
	Kind ActorKind
	Name string
}

func (a Actor) String() string { return fmt.Sprintf("%s:%s", a.Kind, a.Name) }

// CurrentActor works out who is running the command.
//
//	SHERPA_ACTOR=agent:claude or human:charles  explicit override
//	CLAUDECODE=1                                running inside Claude Code → agent:claude
//	CURSOR_AGENT=1                              a Cursor agent's shell → agent:cursor
//	OPENCODE=1                                  an OpenCode agent's shell → agent:opencode
//	CODEX_THREAD_ID set                         a Codex agent's shell → agent:codex
//	otherwise                                   human, named from git or $USER
func CurrentActor() Actor {
	if v := strings.TrimSpace(os.Getenv("SHERPA_ACTOR")); v != "" {
		kind, name, found := strings.Cut(v, ":")
		if !found {
			name = kind
		}
		if ActorKind(kind) == Agent {
			return Actor{Kind: Agent, Name: name}
		}
		if ActorKind(kind) == Human {
			return Actor{Kind: Human, Name: name}
		}
		return Actor{Kind: Agent, Name: v}
	}
	switch {
	case os.Getenv("CLAUDECODE") == "1":
		return Actor{Kind: Agent, Name: "claude"}
	case os.Getenv("CURSOR_AGENT") == "1":
		return Actor{Kind: Agent, Name: "cursor"}
	case os.Getenv("OPENCODE") == "1":
		return Actor{Kind: Agent, Name: "opencode"}
	case os.Getenv("CODEX_THREAD_ID") != "":
		return Actor{Kind: Agent, Name: "codex"}
	}
	name := os.Getenv("USER")
	if out, err := exec.Command("git", "config", "user.name").Output(); err == nil {
		if n := strings.TrimSpace(string(out)); n != "" {
			name = n
		}
	}
	return Actor{Kind: Human, Name: name}
}

// CheckTransition returns an error if a may not move it to status to.
func CheckTransition(a Actor, it *model.Item, to model.Status) error {
	if it.Status == to {
		return fmt.Errorf("%s is already %s", it.ID, to)
	}
	if a.Kind == Agent {
		if to == model.Done {
			return fmt.Errorf("agents cannot mark %s done; use `shp submit`, then `shp done --approved-by <human>` once a human approves", it.ID)
		}
		if it.Status == model.Done {
			return fmt.Errorf("%s is done; only a human can reopen it", it.ID)
		}
	}
	return nil
}

// CheckApprovedDone returns an error if a may not close it on behalf of
// approver. Humans close anything they could close anyway; agents need a
// named human approver and may only close work that is in review.
func CheckApprovedDone(a Actor, it *model.Item, approver string) error {
	if a.Kind != Agent {
		return CheckTransition(a, it, model.Done)
	}
	if strings.TrimSpace(approver) == "" {
		return fmt.Errorf("agents need --approved-by <human> to mark %s done", it.ID)
	}
	if it.Status != model.Review && it.Status != model.AgentReview {
		return fmt.Errorf("%s is %s; agents can only close items in review or agent review", it.ID, it.Status)
	}
	return nil
}
