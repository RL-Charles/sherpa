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
			if it.Kind == model.Ticket {
				return fmt.Errorf("agents close tickets with `shp done %s` once all deliverables and checks are resolved", it.ID)
			}
			return fmt.Errorf("%s is a %s: only a human can close stories and epics after review", it.ID, it.Kind)
		}
		if it.Status == model.Done {
			return fmt.Errorf("%s is done; only a human can reopen it", it.ID)
		}
	}
	return nil
}

// CheckDone is the closure policy. Humans may close any item through the
// ordinary transition rules. Agents may close a ticket once it has been
// submitted and every deliverable and check is resolved. Stories and epics
// are human-only, and an approver name does not change that.
func CheckDone(a Actor, it *model.Item) error {
	if a.Kind != Agent {
		return CheckTransition(a, it, model.Done)
	}
	if it.Kind != model.Ticket {
		return fmt.Errorf("%s is a %s: only a human can close stories and epics after review", it.ID, it.Kind)
	}
	if it.Status != model.Review && it.Status != model.AgentReview {
		return fmt.Errorf("%s is %s: submit it first; agents close tickets from review or agent review", it.ID, statusWord(it.Status))
	}
	if len(it.Deliverables) == 0 {
		return fmt.Errorf("%s has no deliverables: add at least one with `shp deliverable add` before closing", it.ID)
	}
	if len(it.Checks) == 0 {
		return fmt.Errorf("%s has no checks: add at least one with `shp check add` before closing", it.ID)
	}
	doneD, totalD := it.DeliverableCounts()
	resolved, total := it.CheckCounts()
	var open []string
	if doneD < totalD {
		for i, d := range it.Deliverables {
			if !d.Done {
				open = append(open, fmt.Sprintf("deliverable %d: %s", i+1, d.Text))
				if len(open) == 3 {
					break
				}
			}
		}
	}
	if resolved < total {
		for i, c := range it.Checks {
			if !c.Resolved() {
				open = append(open, fmt.Sprintf("check %d: %s", i+1, c.Text))
				if len(open) == 6 {
					break
				}
			}
		}
	}
	if len(open) > 0 {
		return fmt.Errorf("%s is not ready to close: %d/%d deliverables and %d/%d checks resolved. Open: %s",
			it.ID, doneD, totalD, resolved, total, strings.Join(open, "; "))
	}
	return nil
}

func statusWord(s model.Status) string {
	return strings.ReplaceAll(string(s), "_", " ")
}
