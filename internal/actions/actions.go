// Package actions holds the item changes a person makes by hand: moving an
// item to a status, approving or sending back reviewed work, and saving an
// edited body. The TUI and the desktop app (through `shp app`) both call
// these, so a change made in either is checked, saved and journaled the same
// way.
package actions

import (
	"errors"
	"fmt"
	"strings"

	"github.com/RL-Charles/sherpa/internal/gitinfo"
	"github.com/RL-Charles/sherpa/internal/journal"
	"github.com/RL-Charles/sherpa/internal/model"
	"github.com/RL-Charles/sherpa/internal/store"
	"github.com/RL-Charles/sherpa/internal/workflow"
)

// Who is acting and which journal session records it.
type Who struct {
	Actor   workflow.Actor
	Session string
}

func label(s model.Status) string { return strings.ReplaceAll(string(s), "_", " ") }

// SetStatus moves an item to status to under the workflow rules and
// journals it. It returns a one-line description of what happened.
func SetStatus(s *store.Store, who Who, id string, to model.Status) (string, error) {
	it, err := s.Get(id)
	if err != nil {
		return "", err
	}
	if it.Status == to {
		return "", fmt.Errorf("%s is already %s", it.ID, label(to))
	}
	if err := workflow.CheckTransition(who.Actor, it, to); err != nil {
		return "", err
	}
	from := it.Status
	it.Status = to
	if err := s.Save(it); err != nil {
		return "", err
	}
	_ = journal.Append(s.Dir, journal.Event{
		Actor: who.Actor.String(), Session: who.Session, Item: it.ID, Type: "status",
		From: string(from), To: string(to), Commit: gitinfo.Head(s.CodeRoot),
	})
	return fmt.Sprintf("%s %s → %s", it.ID, label(from), label(to)), nil
}

// Reviewable returns the item if it waits for review.
func Reviewable(s *store.Store, id string) (*model.Item, error) {
	it, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if it.Status != model.Review && it.Status != model.AgentReview {
		return nil, fmt.Errorf("%s is %s, not in review", it.ID, label(it.Status))
	}
	return it, nil
}

// Approve closes an item in review. Approving is a human sign-off, so it
// ticks the manual checks still open, naming who verified them.
func Approve(s *store.Store, who Who, id, note string) (string, error) {
	it, err := Reviewable(s, id)
	if err != nil {
		return "", err
	}
	if err := workflow.CheckTransition(who.Actor, it, model.Done); err != nil {
		return "", err
	}
	var ticked []string
	for _, n := range it.OpenManual() {
		c := &it.Checks[n-1]
		c.Done, c.Evidence = true, "verified by "+who.Actor.Name
		ticked = append(ticked, fmt.Sprintf("✋ check %d: %s", n, c.Text))
	}
	from := it.Status
	it.Status = model.Done
	if err := s.Save(it); err != nil {
		return "", err
	}
	if strings.TrimSpace(note) == "" {
		note = "approved from the needs-you inbox"
	}
	_ = journal.Append(s.Dir, journal.Event{
		Actor: who.Actor.String(), Session: who.Session, Item: it.ID, Type: "status",
		From: string(from), To: string(model.Done), Message: note, Detail: ticked,
		Commit: gitinfo.Head(s.CodeRoot),
	})
	return it.ID + " approved · done", nil
}

// SendBack returns reviewed work to in progress with the reviewer's note,
// as `shp review --reject` does.
func SendBack(s *store.Store, who Who, id, note string) (string, error) {
	note = strings.TrimSpace(note)
	if note == "" {
		return "", errors.New("say what needs fixing")
	}
	it, err := Reviewable(s, id)
	if err != nil {
		return "", err
	}
	if err := workflow.CheckTransition(who.Actor, it, model.InProgress); err != nil {
		return "", err
	}
	from := it.Status
	it.Status = model.InProgress
	if err := s.Save(it); err != nil {
		return "", err
	}
	_ = journal.Append(s.Dir, journal.Event{
		Actor: who.Actor.String(), Session: who.Session, Item: id, Type: "review", Kind: "reject",
		From: string(from), To: string(model.InProgress), Message: note, Commit: gitinfo.Head(s.CodeRoot),
	})
	return id + " sent back", nil
}

// ErrConflict means the body changed on disk since editing began in a way
// that cannot be merged; saving again with force overwrites it.
var ErrConflict = errors.New("changed on disk while you edited")

// SaveBody replaces an item's body with body. base is the body as it was
// when editing began. If the body on disk moved on since, and the change is
// only lines appended after base (the common case: an agent adding notes),
// those lines are kept after the edit and merged is true; any other change
// is a conflict unless force is set. Only the body changes, so status or
// assignee changes made meanwhile survive.
func SaveBody(s *store.Store, who Who, id, base, body, via string, force bool) (saved string, merged bool, err error) {
	cur, err := s.Get(id)
	if err != nil {
		return "", false, err
	}
	if cur.Body != base && !force {
		trimmed := strings.TrimRight(base, "\n")
		if !strings.HasPrefix(cur.Body, trimmed) {
			return "", false, fmt.Errorf("%s %w", id, ErrConflict)
		}
		body = strings.TrimRight(body, "\n") + cur.Body[len(trimmed):]
		merged = true
	}
	cur.Body = body
	if err := s.Save(cur); err != nil {
		return "", false, err
	}
	_ = journal.Append(s.Dir, journal.Event{
		Actor: who.Actor.String(), Session: who.Session, Item: cur.ID, Type: "edit", Message: "edited body in " + via,
	})
	return cur.Body, merged, nil
}
