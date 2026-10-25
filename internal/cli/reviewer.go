package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/RL-Charles/sherpa/internal/journal"
	"github.com/RL-Charles/sherpa/internal/model"
	"github.com/RL-Charles/sherpa/internal/policy"
	"github.com/RL-Charles/sherpa/internal/review"
)

// launchReviewer starts the configured reviewer agent on it and records that
// in the journal.
func (a *app) launchReviewer(it *model.Item) error {
	log, err := review.Spawn(a.store, a.store.Config.Reviewer.Command, it.ID)
	if err != nil {
		return err
	}
	if err := a.record(journal.Event{Item: it.ID, Type: "review", Kind: "spawn",
		Message: a.store.Config.Reviewer.Command, Detail: []string{"log: " + log}}); err != nil {
		return err
	}
	if !a.json {
		fmt.Fprintf(os.Stderr, "⟳ independent reviewer launched for %s (log: %s)\n", it.ID, log)
	}
	return nil
}

// autoReview launches the reviewer after an agent submit when the project
// asks for it. Reviewers never launch reviewers. Failure only warns: the
// work is submitted either way.
func (a *app) autoReview(it *model.Item) {
	r := a.store.Config.Reviewer
	if !r.Auto || r.Command == "" || os.Getenv("SHERPA_REVIEW") != "" {
		return
	}
	if err := a.launchReviewer(it); err != nil {
		fmt.Fprintln(os.Stderr, "⚠ reviewer not launched:", err)
	}
}

// selfReview refuses a verdict from the session that submitted the work.
func (a *app) selfReview(id string) error {
	events, err := journal.ReadAll(a.store.Dir)
	if err != nil {
		return err
	}
	author := ""
	for _, e := range journal.ForItem(events, id) {
		if e.Type == "submit" {
			author = e.Session
		}
	}
	if author != "" && author == a.session {
		return fmt.Errorf("this session submitted %s, so it can't review it: run `shp review %s --spawn` for an independent reviewer, or leave it for a human", id, id)
	}
	return nil
}

// policyClose returns "policy:<rule>" when a passing review may close it
// under .sherpa/policy.yaml, else "".
func (a *app) policyClose(it *model.Item, pass bool) (string, error) {
	if !pass {
		return "", nil
	}
	p, err := policy.Load(a.store.Dir)
	if err != nil {
		return "", err
	}
	all, err := a.store.All()
	if err != nil {
		return "", err
	}
	rule, _ := p.Closes(it, all, a.submittedLines(it.ID))
	if rule == "" {
		return "", nil
	}
	return "policy:" + rule, nil
}

// submittedLines is the lines added plus removed by the item's last submit.
func (a *app) submittedLines(id string) int {
	events, err := journal.ReadAll(a.store.Dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range journal.ForItem(events, id) {
		if e.Type == "submit" {
			n = e.Added + e.Removed
		}
	}
	return n
}

func (a *app) policyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "policy [id]",
		Short: "Show which work a passing agent review may close without a human",
		Long: `Show the auto_done rules in .sherpa/policy.yaml, or whether a passing
agent review would close one item and why.

  auto_done:
    - name: docs
      labels: [docs, tests]   # the item has any of these labels
    - name: tooling
      under: [S-008]          # it sits under any of these stories/epics
      max_lines: 200          # and its submit changed at most this many lines

Without the file every item goes to human review. Items with unresolved
checks, manual or not, always do.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			p, err := policy.Load(a.store.Dir)
			if err != nil {
				return err
			}
			if len(args) == 0 {
				if a.json {
					return a.emit(p)
				}
				if len(p.AutoDone) == 0 {
					fmt.Println("No auto_done rules: every agent review that passes goes to a human.")
					return nil
				}
				for _, r := range p.AutoDone {
					var when []string
					if len(r.Labels) > 0 {
						when = append(when, "labels "+strings.Join(r.Labels, "|"))
					}
					if len(r.Under) > 0 {
						when = append(when, "under "+strings.Join(r.Under, "|"))
					}
					if r.MaxLines > 0 {
						when = append(when, fmt.Sprintf("≤ %d lines", r.MaxLines))
					}
					fmt.Printf("%-12s %s\n", r.Name, strings.Join(when, " · "))
				}
				return nil
			}
			it, err := a.store.Get(args[0])
			if err != nil {
				return err
			}
			all, err := a.store.All()
			if err != nil {
				return err
			}
			rule, why := p.Closes(it, all, a.submittedLines(it.ID))
			if a.json {
				return a.emit(map[string]string{"id": it.ID, "rule": rule, "why": why})
			}
			if rule != "" {
				fmt.Printf("%s: a passing agent review closes it (rule %s)\n", it.ID, rule)
			} else {
				fmt.Printf("%s: goes to a human (%s)\n", it.ID, why)
			}
			return nil
		},
	}
}
